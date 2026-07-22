package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/HizKz/codex-history/internal/appserver"
)

type Item struct {
	ID         string
	Kind       string
	Role       string
	Title      string
	Text       string
	Detail     string
	Status     string
	Expandable bool
}

type Transcript struct {
	Thread appserver.Thread
	Items  []Item
	Body   string
}

func Build(thread appserver.Thread) Transcript {
	var items []Item
	var searchable []string
	for _, turn := range thread.Turns {
		for _, raw := range turn.Items {
			item := parseItem(raw)
			items = append(items, item)
			if item.Text != "" {
				searchable = append(searchable, item.Text)
			}
			if item.Kind != "commandExecution" && item.Kind != "mcpToolCall" && item.Kind != "dynamicToolCall" {
				searchable = append(searchable, item.Title)
			} else {
				searchable = append(searchable, item.Title)
			}
		}
	}
	return Transcript{Thread: thread, Items: items, Body: strings.Join(searchable, "\n")}
}

func Title(thread appserver.Thread) string {
	if thread.Name != nil && strings.TrimSpace(*thread.Name) != "" {
		return oneLine(*thread.Name)
	}
	if strings.TrimSpace(thread.Preview) != "" {
		return oneLine(thread.Preview)
	}
	if len(thread.ID) > 12 {
		return thread.ID[:12]
	}
	return thread.ID
}

func oneLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= 120 {
		return value
	}
	runes := []rune(value)
	return string(runes[:117]) + "..."
}

func parseItem(raw json.RawMessage) Item {
	var base struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return Item{Kind: "unknown", Title: "Unreadable item", Detail: string(raw), Expandable: true}
	}
	item := Item{ID: base.ID, Kind: base.Type, Status: base.Status}
	switch base.Type {
	case "userMessage":
		var value struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Path string `json:"path"`
				URL  string `json:"url"`
				Name string `json:"name"`
			} `json:"content"`
		}
		_ = json.Unmarshal(raw, &value)
		var parts []string
		for _, content := range value.Content {
			switch content.Type {
			case "text":
				parts = append(parts, content.Text)
			case "localImage", "image":
				parts = append(parts, "[image]")
			case "skill", "mention":
				parts = append(parts, "["+content.Type+": "+content.Name+"]")
			}
		}
		item.Role, item.Title, item.Text = "user", "You", strings.Join(parts, "\n")
	case "agentMessage":
		var value struct {
			Text  string `json:"text"`
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Role, item.Title, item.Text = "assistant", "Codex", value.Text
		if value.Phase != "" {
			item.Status = value.Phase
		}
	case "plan":
		var value struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Role, item.Title, item.Text = "assistant", "Plan", value.Text
	case "reasoning":
		var value struct {
			Summary []string `json:"summary"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Title, item.Text = "Reasoning", strings.Join(value.Summary, "\n")
	case "commandExecution":
		var value struct {
			Command          string `json:"command"`
			CWD              string `json:"cwd"`
			AggregatedOutput string `json:"aggregatedOutput"`
			ExitCode         *int   `json:"exitCode"`
			DurationMS       *int64 `json:"durationMs"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Title = "$ " + value.Command
		item.Text = value.CWD
		item.Detail = value.AggregatedOutput
		item.Expandable = item.Detail != ""
		if value.ExitCode != nil {
			item.Status = fmt.Sprintf("exit %d", *value.ExitCode)
		}
	case "fileChange":
		var value struct {
			Changes []struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			} `json:"changes"`
		}
		_ = json.Unmarshal(raw, &value)
		var paths []string
		for _, change := range value.Changes {
			paths = append(paths, change.Kind+" "+change.Path)
		}
		item.Title = fmt.Sprintf("File changes (%d)", len(value.Changes))
		item.Text = strings.Join(paths, "\n")
		item.Detail = prettyJSON(raw)
		item.Expandable = true
	case "mcpToolCall":
		var value struct {
			Server    string          `json:"server"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
			Result    json.RawMessage `json:"result"`
			Error     json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Title = "MCP " + value.Server + "/" + value.Tool
		item.Detail = joinJSON(value.Arguments, value.Result, value.Error)
		item.Expandable = item.Detail != ""
	case "dynamicToolCall":
		var value struct {
			Namespace string          `json:"namespace"`
			Tool      string          `json:"tool"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Title = strings.Trim(value.Namespace+"/"+value.Tool, "/")
		item.Detail = prettyJSON(value.Arguments)
		item.Expandable = item.Detail != ""
	case "webSearch":
		var value struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Title, item.Text = "Web search", value.Query
	case "imageView":
		var value struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(raw, &value)
		item.Title, item.Text = "Viewed image", filepath.Base(value.Path)
	case "contextCompaction":
		item.Title = "Context compacted"
	default:
		item.Title = displayKind(base.Type)
		item.Detail = prettyJSON(raw)
		item.Expandable = true
	}
	if item.Title == "" {
		item.Title = displayKind(base.Type)
	}
	return item
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func joinJSON(values ...json.RawMessage) string {
	var parts []string
	for _, value := range values {
		if text := prettyJSON(value); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func displayKind(kind string) string {
	if kind == "" {
		return "Unknown item"
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

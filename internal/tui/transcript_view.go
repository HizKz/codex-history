package tui

import (
	"fmt"
	"strings"

	"github.com/HizKz/codex-history/internal/history"
)

type transcriptView int

const (
	transcriptConversation transcriptView = iota
	transcriptActivity
	transcriptDetail
)

type transcriptLineRole int

const (
	lineBody transcriptLineRole = iota
	lineMuted
	lineUser
	lineAssistant
	lineActivity
)

type transcriptLine struct {
	Text       string
	Role       transcriptLineRole
	Turn       int
	TurnStart  bool
	Activity   bool
	Actionable bool
}

func buildTranscriptLines(transcript history.Transcript, width int, showActivityEvents bool) []transcriptLine {
	width = max(4, width)
	var lines []transcriptLine
	appendLine := func(line transcriptLine) {
		lines = append(lines, line)
	}
	appendSpacing := func(turn int) {
		if len(lines) > 0 && lines[len(lines)-1].Text != "" {
			appendLine(transcriptLine{Text: "", Role: lineMuted, Turn: turn})
		}
	}
	appendText := func(text string, role transcriptLineRole, turn int) {
		wrapped := wrapLines(text, width)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for _, line := range wrapped {
			appendLine(transcriptLine{Text: line, Role: role, Turn: turn})
		}
	}
	appendMessage := func(item history.Item, turn int) {
		role := lineBody
		if item.Role == "user" {
			role = lineUser
		} else if item.Role == "assistant" {
			role = lineAssistant
		}
		appendSpacing(turn)
		appendLine(transcriptLine{Text: strings.ToUpper(item.Title), Role: role, Turn: turn})
		appendText(item.Text, lineBody, turn)
	}

	for turnIndex, turn := range transcript.Turns {
		appendSpacing(turnIndex)
		heading := fmt.Sprintf("TURN %d", turnIndex+1)
		if turn.Status != "" {
			heading += " · " + turn.Status
		}
		appendLine(transcriptLine{
			Text: heading, Role: lineMuted, Turn: turnIndex, TurnStart: true,
		})

		for _, item := range turn.Primary {
			if item.Role == "user" {
				appendMessage(item, turnIndex)
			}
		}
		if len(turn.Activity) > 0 {
			appendSpacing(turnIndex)
			appendLine(transcriptLine{
				Text:       activitySummary(turn.Activity),
				Role:       lineActivity,
				Turn:       turnIndex,
				Activity:   true,
				Actionable: true,
			})
			if showActivityEvents {
				for i, item := range turn.Activity {
					appendLine(transcriptLine{
						Text: "  " + activityEventLabel(i, item),
						Role: lineMuted,
						Turn: turnIndex,
					})
				}
			}
		}
		for _, item := range turn.Primary {
			if item.Role != "user" {
				appendMessage(item, turnIndex)
			}
		}
	}
	return lines
}

func activitySummary(items []history.Item) string {
	counts := map[string]int{}
	for _, item := range items {
		switch item.Kind {
		case "commandExecution":
			counts["cmd"]++
		case "fileChange":
			counts["files"]++
		case "mcpToolCall", "dynamicToolCall":
			counts["tools"]++
		case "plan":
			counts["plan"]++
		case "reasoning":
			counts["reasoning"]++
		case "agentMessage":
			counts["updates"]++
		case "webSearch":
			counts["web"]++
		default:
			counts["other"]++
		}
	}
	parts := []string{fmt.Sprintf("▶ Activity  %d", len(items))}
	for _, key := range []string{"cmd", "files", "tools", "plan", "reasoning", "updates", "web", "other"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", key, counts[key]))
		}
	}
	return strings.Join(parts, " · ")
}

func activityEventLabel(index int, item history.Item) string {
	label := fmt.Sprintf("%02d  %s", index+1, item.Title)
	if item.Status != "" {
		label += "  [" + item.Status + "]"
	}
	return label
}

func detailLines(item history.Item, width int) []string {
	width = max(4, width)
	title := item.Title
	if item.Status != "" {
		title += "  [" + item.Status + "]"
	}
	lines := wrapLines(title, width)
	if item.Text != "" {
		lines = append(lines, "")
		lines = append(lines, wrapLines(item.Text, width)...)
	}
	if item.Detail != "" {
		lines = append(lines, "")
		lines = append(lines, wrapLines(item.Detail, width)...)
	}
	if len(lines) == 0 {
		return []string{"No additional details"}
	}
	return lines
}

func visibleStart(offset, cursor, rows, total int) int {
	rows = max(1, rows)
	total = max(0, total)
	if total <= rows {
		return 0
	}
	offset = min(max(offset, 0), total-rows)
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+rows {
		offset = cursor - rows + 1
	}
	return min(max(offset, 0), total-rows)
}

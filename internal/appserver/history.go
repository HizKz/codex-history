package appserver

import (
	"context"
	"encoding/json"
	"fmt"
)

type Thread struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"sessionId"`
	Name           *string         `json:"name"`
	Preview        string          `json:"preview"`
	CWD            string          `json:"cwd"`
	Source         string          `json:"source"`
	ThreadSource   json.RawMessage `json:"threadSource"`
	ModelProvider  string          `json:"modelProvider"`
	CLIVersion     string          `json:"cliVersion"`
	CreatedAt      int64           `json:"createdAt"`
	UpdatedAt      int64           `json:"updatedAt"`
	RecencyAt      *int64          `json:"recencyAt"`
	ParentThreadID *string         `json:"parentThreadId"`
	ForkedFromID   *string         `json:"forkedFromId"`
	AgentNickname  *string         `json:"agentNickname"`
	AgentRole      *string         `json:"agentRole"`
	Status         json.RawMessage `json:"status"`
	Turns          []Turn          `json:"turns"`
	Archived       bool            `json:"-"`
}

type Turn struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	StartedAt   *int64            `json:"startedAt"`
	CompletedAt *int64            `json:"completedAt"`
	DurationMS  *int64            `json:"durationMs"`
	Error       json.RawMessage   `json:"error"`
	Items       []json.RawMessage `json:"items"`
	ItemsView   string            `json:"itemsView"`
}

type ListOptions struct {
	SourceKinds   []string
	Archived      bool
	Limit         int
	SortKey       string
	SortDirection string
	SearchTerm    string
}

type listResponse struct {
	Data       []Thread `json:"data"`
	NextCursor *string  `json:"nextCursor"`
}

func (c *Client) ListThreads(ctx context.Context, opts ListOptions) ([]Thread, error) {
	var all []Thread
	var cursor *string
	for {
		params := map[string]any{
			"sourceKinds":    opts.SourceKinds,
			"archived":       opts.Archived,
			"limit":          opts.Limit,
			"sortKey":        opts.SortKey + "_at",
			"sortDirection":  opts.SortDirection,
			"useStateDbOnly": true,
		}
		if opts.SortKey == "created" || opts.SortKey == "updated" {
			params["sortKey"] = opts.SortKey + "_at"
		}
		if opts.SearchTerm != "" {
			params["searchTerm"] = opts.SearchTerm
		}
		if cursor != nil {
			params["cursor"] = *cursor
		}
		var page listResponse
		if err := c.Request(ctx, "thread/list", params, &page); err != nil {
			return nil, err
		}
		for i := range page.Data {
			page.Data[i].Archived = opts.Archived
		}
		all = append(all, page.Data...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return all, nil
}

func (c *Client) ReadThread(ctx context.Context, id string) (Thread, error) {
	var result struct {
		Thread Thread `json:"thread"`
	}
	if err := c.Request(ctx, "thread/read", map[string]any{
		"threadId":     id,
		"includeTurns": true,
	}, &result); err != nil {
		return Thread{}, err
	}
	if result.Thread.ID == "" {
		return Thread{}, fmt.Errorf("thread/read returned an empty thread for %s", id)
	}
	return result.Thread, nil
}

func SourceKinds(sources []string) []string {
	mapping := map[string][]string{
		"cli":        {"cli"},
		"vscode":     {"vscode"},
		"app_server": {"appServer"},
		"exec":       {"exec"},
		"sub_agent":  {"subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther"},
		"unknown":    {"unknown"},
	}
	var kinds []string
	for _, source := range sources {
		kinds = append(kinds, mapping[source]...)
	}
	return kinds
}

func AllSourceKinds() []string {
	return []string{"cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown"}
}

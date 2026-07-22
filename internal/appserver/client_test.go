package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestClientLifecycle(t *testing.T) {
	if os.Getenv("CODEX_HISTORY_APP_SERVER_HELPER") == "1" {
		runHelperServer()
		os.Exit(0)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex")
	content := "#!/bin/sh\nexec \"$CODEX_HISTORY_HELPER_BIN\" -test.run=TestClientLifecycle\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HISTORY_APP_SERVER_HELPER", "1")
	t.Setenv("CODEX_HISTORY_HELPER_BIN", os.Args[0])
	client, err := Start(context.Background(), script)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	threads, err := client.ListThreads(context.Background(), ListOptions{
		SourceKinds: []string{"cli"}, Limit: 10, SortKey: "updated", SortDirection: "desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != "thread-1" {
		t.Fatalf("unexpected threads: %#v", threads)
	}
	thread, err := client.ReadThread(context.Background(), threads[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Turns) != 1 || len(thread.Turns[0].Items) != 1 {
		t.Fatalf("unexpected full thread: %#v", thread)
	}
}

func runHelperServer() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"userAgent": "fake-codex"}
		case "thread/list":
			result = map[string]any{
				"data": []any{map[string]any{
					"id": "thread-1", "preview": "hello", "cwd": "/tmp",
					"source": "cli", "createdAt": 1, "updatedAt": 2, "turns": []any{},
				}},
				"nextCursor": nil,
			}
		case "thread/read":
			result = map[string]any{"thread": map[string]any{
				"id": "thread-1", "preview": "hello", "cwd": "/tmp", "source": "cli",
				"createdAt": 1, "updatedAt": 2,
				"turns": []any{map[string]any{"id": "turn-1", "status": "completed", "items": []any{
					map[string]any{"id": "message-1", "type": "agentMessage", "text": "hello"},
				}}},
			}}
		default:
			_ = encoder.Encode(map[string]any{"id": *request.ID, "error": map[string]any{"code": -32601, "message": fmt.Sprintf("unknown %s", request.Method)}})
			continue
		}
		_ = encoder.Encode(map[string]any{"id": *request.ID, "result": result})
	}
}

func TestSourceKinds(t *testing.T) {
	got := SourceKinds([]string{"cli", "sub_agent"})
	if len(got) != 6 || got[0] != "cli" || got[1] != "subAgent" {
		t.Fatalf("unexpected source kinds: %#v", got)
	}
}

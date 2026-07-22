package history

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HizKz/codex-history/internal/appserver"
)

func TestBuildTranscript(t *testing.T) {
	thread := appserver.Thread{
		ID: "thread-1", Preview: "hello", CWD: "/tmp/project",
		Turns: []appserver.Turn{{Items: []json.RawMessage{
			json.RawMessage(`{"id":"u1","type":"userMessage","content":[{"type":"text","text":"find the parser"}]}`),
			json.RawMessage(`{"id":"a1","type":"agentMessage","text":"Found it.","phase":"final_answer"}`),
			json.RawMessage(`{"id":"c1","type":"commandExecution","command":"rg parser","cwd":"/tmp/project","aggregatedOutput":"secret output","exitCode":0}`),
		}}},
	}
	transcript := Build(thread)
	if len(transcript.Items) != 3 {
		t.Fatalf("got %d items", len(transcript.Items))
	}
	if transcript.Items[0].Role != "user" || transcript.Items[1].Role != "assistant" {
		t.Fatalf("unexpected roles: %#v", transcript.Items)
	}
	if !strings.Contains(transcript.Body, "find the parser") || !strings.Contains(transcript.Body, "rg parser") {
		t.Fatalf("missing searchable content: %q", transcript.Body)
	}
	if strings.Contains(transcript.Body, "secret output") {
		t.Fatal("command output must not enter the search index")
	}
}

func TestTitlePreference(t *testing.T) {
	name := " Named conversation "
	if got := Title(appserver.Thread{Name: &name, Preview: "fallback"}); got != "Named conversation" {
		t.Fatalf("got %q", got)
	}
}

func TestPrettyJSON(t *testing.T) {
	got := prettyJSON(json.RawMessage(`{"a":1}`))
	if !strings.Contains(got, "\n") {
		t.Fatalf("expected indented JSON, got %q", got)
	}
}

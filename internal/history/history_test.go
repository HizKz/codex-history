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
	if len(transcript.Turns) != 1 || len(transcript.Turns[0].Primary) != 2 || len(transcript.Turns[0].Activity) != 1 {
		t.Fatalf("unexpected turn grouping: %#v", transcript.Turns)
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

func TestBuildNormalizesFileChangesWithoutIndexingDiffsOrPaths(t *testing.T) {
	thread := appserver.Thread{
		ID: "thread-1",
		Turns: []appserver.Turn{{Items: []json.RawMessage{
			json.RawMessage(`{"id":"u1","type":"userMessage","content":[{"type":"text","text":"update the parser"}]}`),
			json.RawMessage(`{"id":"f1","type":"fileChange","status":"completed","changes":[{"path":"/synthetic/project/parser.go","kind":"update","diff":"@@ -1 +1 @@\n-old\n+new"},{"path":"docs/guide.md","kind":"add","diff":"+guide"}]}`),
		}}},
	}

	transcript := Build(thread)
	item := transcript.Turns[0].Activity[0]
	if len(item.FileChanges) != 2 {
		t.Fatalf("file changes = %#v", item.FileChanges)
	}
	if got := item.FileChanges[0]; got.Path != "/synthetic/project/parser.go" || got.Kind != "update" ||
		!strings.Contains(got.Diff, "+new") {
		t.Fatalf("first file change = %#v", got)
	}
	if !strings.Contains(item.Detail, "update /synthetic/project/parser.go") ||
		!strings.Contains(item.Detail, "add docs/guide.md") ||
		!strings.Contains(item.Detail, "@@ -1 +1 @@") {
		t.Fatalf("formatted detail = %q", item.Detail)
	}
	for _, private := range []string{"/synthetic/project/parser.go", "docs/guide.md", "-old", "+new", "+guide"} {
		if strings.Contains(transcript.Body, private) {
			t.Fatalf("search body contains file-change detail %q: %q", private, transcript.Body)
		}
	}
	if !strings.Contains(transcript.Body, "File changes (2)") {
		t.Fatalf("search body is missing safe file-change title: %q", transcript.Body)
	}
}

func TestBuildPromotesLastAgentMessageWhenFinalAnswerIsMissing(t *testing.T) {
	thread := appserver.Thread{
		ID: "thread-1",
		Turns: []appserver.Turn{{Items: []json.RawMessage{
			json.RawMessage(`{"id":"u1","type":"userMessage","content":[{"type":"text","text":"keep going"}]}`),
			json.RawMessage(`{"id":"a1","type":"agentMessage","text":"First update.","phase":"commentary"}`),
			json.RawMessage(`{"id":"c1","type":"commandExecution","command":"go test ./...","exitCode":0}`),
			json.RawMessage(`{"id":"a2","type":"agentMessage","text":"Latest update.","phase":"commentary"}`),
		}}},
	}

	turn := Build(thread).Turns[0]
	if len(turn.Primary) != 2 || turn.Primary[1].Text != "Latest update." {
		t.Fatalf("last agent message was not promoted: %#v", turn.Primary)
	}
	if len(turn.Activity) != 2 || turn.Activity[0].Text != "First update." {
		t.Fatalf("unexpected activity after promotion: %#v", turn.Activity)
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

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/HizKz/codex-history/internal/config"
	"github.com/HizKz/codex-history/internal/history"
)

func TestShellCommandQuotesArguments(t *testing.T) {
	got := shellCommand("/Applications/Codex CLI/codex", "resume", "thread id")
	if got != "'/Applications/Codex CLI/codex' resume 'thread id'" {
		t.Fatalf("got %q", got)
	}
}

func TestWrapLinesPreservesContent(t *testing.T) {
	lines := wrapLines("abcdefgh", 3)
	if strings.Join(lines, "") != "abcdefgh" || len(lines) != 3 {
		t.Fatalf("unexpected wrap: %#v", lines)
	}
}

func TestJapaneseDisplayWidth(t *testing.T) {
	if got := truncate("日本語の会話", 7); got != "日本語…" {
		t.Fatalf("truncate = %q", got)
	}
	lines := wrapLines("日本語の会話", 6)
	if strings.Join(lines, "") != "日本語の会話" || len(lines) != 2 {
		t.Fatalf("unexpected Japanese wrap: %#v", lines)
	}
}

func TestTranscriptNavigationAndInspector(t *testing.T) {
	m := transcriptTestModel()
	lines := m.currentTranscriptLines()
	for i, line := range lines {
		if line.Actionable {
			m.transcriptCursor = i
			break
		}
	}

	next, _ := m.handleTranscriptKey("space")
	m = next.(model)
	if m.transcriptView != transcriptActivity || m.activityTurn != 0 {
		t.Fatalf("activity did not open: %#v", m)
	}

	next, _ = m.handleTranscriptKey("enter")
	m = next.(model)
	if m.transcriptView != transcriptDetail {
		t.Fatalf("detail did not open: %#v", m.transcriptView)
	}

	next, _ = m.handleTranscriptKey("esc")
	m = next.(model)
	if m.transcriptView != transcriptActivity {
		t.Fatalf("detail did not return to activity: %#v", m.transcriptView)
	}

	next, _ = m.handleTranscriptKey("esc")
	m = next.(model)
	if m.transcriptView != transcriptConversation {
		t.Fatalf("activity did not return to conversation: %#v", m.transcriptView)
	}
}

func TestTranscriptUsesConfiguredScrollKeys(t *testing.T) {
	m := transcriptTestModel()
	m.cfg.Keys.Transcript.Down = []string{"x"}

	next, _ := m.handleTranscriptKey("x")
	got := next.(model)
	if got.transcriptCursor != 1 {
		t.Fatalf("custom down key moved to %d", got.transcriptCursor)
	}
}

func TestTranscriptTurnJumpAndResizeAnchor(t *testing.T) {
	m := transcriptTestModel()
	lines := m.currentTranscriptLines()
	m.transcriptCursor = nextTurnLine(lines, 0)
	if lines[m.transcriptCursor].Turn != 1 {
		t.Fatalf("next turn selected turn %d", lines[m.transcriptCursor].Turn)
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = next.(model)
	resized := m.currentTranscriptLines()
	if resized[m.transcriptCursor].Turn != 1 {
		t.Fatalf("resize moved to turn %d", resized[m.transcriptCursor].Turn)
	}
}

func TestRenderShowsConversationFirstLayout(t *testing.T) {
	m := transcriptTestModel()
	rendered := m.render()
	for _, want := range []string{"▶ Transcript", "TURN 1", "YOU", "▶ Activity", "CODEX", "j/k scroll"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("render missing %q:\n%s", want, rendered)
		}
	}

	m.width = 80
	rendered = m.render()
	if !strings.Contains(rendered, "Transcript 2/2") || strings.Contains(rendered, "Conversations 1/2") {
		t.Fatalf("compact transcript view is unclear:\n%s", rendered)
	}
}

func TestRenderActivityAndDetailViews(t *testing.T) {
	m := transcriptTestModel()
	lines := m.currentTranscriptLines()
	for i, line := range lines {
		if line.Actionable {
			m.transcriptCursor = i
			break
		}
	}
	next, _ := m.handleTranscriptKey("space")
	m = next.(model)
	rendered := m.render()
	for _, want := range []string{"Activity · Turn 1", "01 Plan", "02 $ rg parser", "enter open", "esc back"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("activity render missing %q:\n%s", want, rendered)
		}
	}

	m.activityCursor = 1
	next, _ = m.handleTranscriptKey("enter")
	m = next.(model)
	rendered = m.render()
	for _, want := range []string{"Detail · $ rg parser", "internal/config/config.go", "ctrl+u/ctrl+d page", "esc back"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("detail render missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderSearchErrorAndHelpStates(t *testing.T) {
	m := transcriptTestModel()
	m.searching, m.query = true, "parser"
	rendered := m.render()
	if !strings.Contains(rendered, "search: parser▌") || !strings.Contains(rendered, "enter accept") {
		t.Fatalf("search state is unclear:\n%s", rendered)
	}

	m.searching, m.err = false, "synthetic failure"
	rendered = m.render()
	if !strings.Contains(rendered, "error: synthetic failure") {
		t.Fatalf("error state is unclear:\n%s", rendered)
	}

	m.err, m.showHelp = "", true
	rendered = m.render()
	for _, want := range []string{"GLOBAL", "TRANSCRIPT", "Open activity"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("help render missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "ACTIVITY") || strings.Contains(rendered, "DETAIL") {
		t.Fatalf("transcript help includes unrelated scopes:\n%s", rendered)
	}

	m.transcriptView = transcriptActivity
	rendered = m.render()
	if !strings.Contains(rendered, "ACTIVITY") || strings.Contains(rendered, "TRANSCRIPT") {
		t.Fatalf("activity help is not contextual:\n%s", rendered)
	}

	m.transcriptView = transcriptDetail
	rendered = m.render()
	if !strings.Contains(rendered, "DETAIL") || strings.Contains(rendered, "ACTIVITY") {
		t.Fatalf("detail help is not contextual:\n%s", rendered)
	}
}

func TestLongTranscriptCanReachLastLine(t *testing.T) {
	m := transcriptTestModel()
	m.transcript.Turns[0].Primary[0].Text = strings.Repeat("日本語", 80)
	lines := m.currentTranscriptLines()
	for range len(lines) + 10 {
		next, _ := m.handleTranscriptKey("j")
		m = next.(model)
	}
	if m.transcriptCursor != len(lines)-1 {
		t.Fatalf("cursor stopped at %d of %d", m.transcriptCursor, len(lines))
	}
}

func transcriptTestModel() model {
	cfg := config.Defaults()
	return model{
		cfg:          cfg,
		width:        120,
		height:       24,
		focus:        focusTranscript,
		transcriptID: "thread-1",
		status:       "2 conversations",
		transcript: history.Transcript{Turns: []history.Turn{
			{
				ID: "turn-1",
				Primary: []history.Item{
					{ID: "user-1", Kind: "userMessage", Role: "user", Title: "You", Text: "Please inspect the parser."},
					{ID: "agent-1", Kind: "agentMessage", Role: "assistant", Title: "Codex", Text: "The parser is in config.go."},
				},
				Activity: []history.Item{
					{ID: "plan-1", Kind: "plan", Title: "Plan", Text: "Inspect the config package."},
					{ID: "cmd-1", Kind: "commandExecution", Title: "$ rg parser", Text: "project", Detail: "internal/config/config.go", Status: "exit 0", Expandable: true},
				},
			},
			{
				ID: "turn-2",
				Primary: []history.Item{
					{ID: "user-2", Kind: "userMessage", Role: "user", Title: "You", Text: "Thanks."},
					{ID: "agent-2", Kind: "agentMessage", Role: "assistant", Title: "Codex", Text: "You're welcome."},
				},
			},
		}},
	}
}

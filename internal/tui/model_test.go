package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/config"
	"github.com/HizKz/codex-history/internal/history"
	"github.com/HizKz/codex-history/internal/index"
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

func TestRenderListUsesTwoLineConversationEntries(t *testing.T) {
	m := transcriptTestModel()
	m.focus = focusList
	m.cfg.UI.ShowTimestamps = false
	m.visible = []appserver.Thread{
		{ID: "thread-1", Preview: "Parser investigation", CWD: "/work/config-parser", Source: "cli"},
		{ID: "thread-2", Preview: "日本語の長い会話タイトル", CWD: "/work/日本語プロジェクト", Source: "vscode", Archived: true},
	}
	m.selected = 1

	rendered := stripSGR(m.renderList(48, 12))
	for _, want := range []string{
		"  Parser investigation",
		"  config-parser · cli",
		"› [A] 日本語の長い会話タイトル",
		"│ 日本語プロジェクト · vscode",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("conversation list missing %q:\n%s", want, rendered)
		}
	}
}

func stripSGR(value string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(value, "")
}

func TestConversationListStylesEmphasizeTitles(t *testing.T) {
	m := transcriptTestModel()
	if style := m.conversationListTitleStyle(false); !style.GetBold() || style.GetUnderline() {
		t.Fatalf("unselected title style = %#v", style)
	}
	if style := m.conversationListTitleStyle(true); !style.GetBold() || !style.GetUnderline() {
		t.Fatalf("selected title style = %#v", style)
	}
	if style := m.conversationListMetadataStyle(false); !style.GetFaint() {
		t.Fatalf("unselected metadata style = %#v", style)
	}
	if style := m.conversationListMetadataStyle(true); style.GetFaint() {
		t.Fatalf("selected metadata should not be faint: %#v", style)
	}
}

func TestConversationListPageSizeUsesCompleteEntries(t *testing.T) {
	if got := conversationListPageSize(9); got != 4 {
		t.Fatalf("page size = %d, want 4", got)
	}
	if got := conversationListPageSize(1); got != 1 {
		t.Fatalf("single-row page size = %d, want 1", got)
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

func TestTranscriptSeparatesConversationBlocks(t *testing.T) {
	lines := transcriptTestModel().currentTranscriptLines()
	var text []string
	for _, line := range lines {
		text = append(text, line.Text)
	}

	want := []string{
		"TURN 1",
		"",
		"YOU",
		"Please inspect the parser.",
		"",
		"▶ Activity  2 · cmd 1 · plan 1",
		"",
		"CODEX",
		"The parser is in config.go.",
		"",
		"TURN 2",
		"",
		"YOU",
		"Thanks.",
		"",
		"CODEX",
		"You're welcome.",
	}
	if strings.Join(text, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected transcript spacing:\ngot:  %#v\nwant: %#v", text, want)
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

func TestSearchKeepsRankedResultsAndRendersMatchContext(t *testing.T) {
	m := transcriptTestModel()
	m.focus = focusList
	m.query = "日本語"
	m.threads = []appserver.Thread{
		{ID: "body", Preview: "Body match", CWD: "/work/alpha"},
		{ID: "title", Preview: "Title match", CWD: "/work/beta"},
	}
	m.searchResults = []index.Result{
		{
			ID: "title", Match: index.MatchTitle,
			Snippet: []index.SnippetSegment{{Text: "before "}, {Text: "日本語", Matched: true}, {Text: " after"}},
		},
		{
			ID: "body", Match: index.MatchBody,
			Snippet: []index.SnippetSegment{{Text: "message "}, {Text: "日本語", Matched: true}},
		},
	}
	m.applyCurrentFilters()
	if len(m.visible) != 2 || m.visible[0].ID != "title" || m.visible[1].ID != "body" {
		t.Fatalf("ranked visible threads = %#v", m.visible)
	}
	rendered := stripSGR(m.renderList(48, 10))
	for _, want := range []string{"title · before 日本語 after", "message · message 日本語"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("search result missing %q:\n%s", want, rendered)
		}
	}
}

func TestSearchIgnoresStaleAsyncResults(t *testing.T) {
	m := transcriptTestModel()
	m.query, m.searchGeneration = "current", 2
	m.threads = []appserver.Thread{{ID: "current"}, {ID: "stale"}}
	m.visible = m.threads

	next, cmd := m.Update(searchMsg{
		query: "stale", generation: 1,
		results: []index.Result{{ID: "stale"}},
	})
	got := next.(model)
	if cmd != nil || len(got.visible) != 2 {
		t.Fatalf("stale result changed model: visible=%#v cmd=%v", got.visible, cmd)
	}

	_, cmd = got.Update(searchDelayMsg{query: "stale", generation: 1})
	if cmd != nil {
		t.Fatal("stale debounce scheduled a search")
	}
}

func TestProjectPickerFiltersExactCWDAndCombinesWithSearch(t *testing.T) {
	m := transcriptTestModel()
	m.focus = focusList
	m.threads = []appserver.Thread{
		{ID: "one", Preview: "Needle one", CWD: "/work/one/demo"},
		{ID: "two", Preview: "Needle two", CWD: "/work/two/demo"},
		{ID: "other", Preview: "Other", CWD: "/work/other"},
	}
	m.visible = m.threads

	next, _ := m.handleKey(tea.KeyPressMsg{Code: 'p'})
	m = next.(model)
	if !m.projectOpen {
		t.Fatal("project picker did not open")
	}
	options := m.projectOptions()
	if len(options) != 4 || options[0].CWD != "" || options[1].CWD != "/work/one/demo" || options[2].CWD != "/work/two/demo" {
		t.Fatalf("project options = %#v", options)
	}

	m.projectCursor = 2
	next, _ = m.handleProjectKey("enter")
	m = next.(model)
	if m.projectCWD != "/work/two/demo" || len(m.visible) != 1 || m.visible[0].ID != "two" {
		t.Fatalf("project filter = %q, visible=%#v", m.projectCWD, m.visible)
	}

	m.query = "Needle"
	m.searchResults = []index.Result{
		{ID: "one", Match: index.MatchPreview, Snippet: []index.SnippetSegment{{Text: "Needle", Matched: true}}},
		{ID: "two", Match: index.MatchPreview, Snippet: []index.SnippetSegment{{Text: "Needle", Matched: true}}},
	}
	m.applyCurrentFilters()
	if len(m.visible) != 1 || m.visible[0].ID != "two" {
		t.Fatalf("combined project search = %#v", m.visible)
	}
}

func TestProjectPickerRenderAndMissingProjectReset(t *testing.T) {
	m := transcriptTestModel()
	m.threads = []appserver.Thread{
		{ID: "jp", CWD: "/work/日本語プロジェクト"},
		{ID: "other", CWD: "/work/other"},
	}
	m.openProjectPicker()
	rendered := stripSGR(m.render())
	for _, want := range []string{"Select project", "All projects", "日本語プロジェクト", "enter select", "esc cancel"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("project picker missing %q:\n%s", want, rendered)
		}
	}

	m.projectCWD = "/work/missing"
	if !m.clearMissingProject() || m.projectCWD != "" {
		t.Fatalf("missing project was not reset: %q", m.projectCWD)
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

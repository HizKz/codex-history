package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

func TestRenderListKeepsContentLeftAlignedWithinMarkerRail(t *testing.T) {
	m := transcriptTestModel()
	m.focus = focusList
	m.cfg.UI.ShowTimestamps = false
	m.visible = []appserver.Thread{
		{ID: "thread-1", Preview: "Parser investigation", CWD: "/work/config-parser", Source: "cli"},
		{ID: "thread-2", Preview: "Selected conversation", CWD: "/work/selected-project", Source: "vscode"},
	}
	m.selected = 1

	rendered := stripSGR(m.renderList(48, 12))
	for _, target := range []string{"Parser investigation", "config-parser", "Selected conversation", "selected-project"} {
		found := false
		for _, line := range strings.Split(rendered, "\n") {
			index := strings.Index(line, target)
			if index < 0 {
				continue
			}
			found = true
			if column := lipgloss.Width(line[:index]); column != 3 {
				t.Fatalf("%q starts at column %d, want 3:\n%s", target, column, rendered)
			}
		}
		if !found {
			t.Fatalf("rendered list missing %q:\n%s", target, rendered)
		}
	}
	for _, line := range strings.Split(rendered, "\n") {
		if width := lipgloss.Width(line); width > 48 {
			t.Fatalf("list line width = %d, want <= 48: %q", width, line)
		}
	}

	for _, focus := range []focus{focusList, focusTranscript} {
		m.focus = focus
		rendered = stripSGR(m.renderList(48, 12))
		for _, line := range strings.Split(rendered, "\n") {
			index := strings.Index(line, "Conversations")
			if index < 0 {
				continue
			}
			if column := lipgloss.Width(line[:index]); column != 3 {
				t.Fatalf("panel title starts at column %d with focus %v, want 3:\n%s", column, focus, rendered)
			}
		}
	}

	m.query = "parser"
	m.searchMatches = map[string]index.Result{
		"thread-1": {
			ID: "thread-1", Match: index.MatchBody,
			Snippet: []index.SnippetSegment{{Text: "before parser after"}},
		},
	}
	rendered = stripSGR(m.renderList(48, 12))
	for _, line := range strings.Split(rendered, "\n") {
		index := strings.Index(line, "message · before parser after")
		if index < 0 {
			continue
		}
		if column := lipgloss.Width(line[:index]); column != 3 {
			t.Fatalf("search context starts at column %d, want 3:\n%s", column, rendered)
		}
		return
	}
	t.Fatalf("rendered search context is missing:\n%s", rendered)
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
	if len(lines) != 21 {
		t.Fatalf("line count = %d, want 21: %#v", len(lines), lines)
	}
	checks := []struct {
		index     int
		kind      transcriptLineKind
		text      string
		alignment transcriptLineAlignment
	}{
		{0, lineTurnDivider, "TURN 1", alignCenter},
		{1, lineBlank, "", alignLeft},
		{2, lineBubbleTop, "You", alignRight},
		{3, lineBubbleBody, "Please inspect the parser.", alignRight},
		{4, lineBubbleBottom, "", alignRight},
		{6, lineText, "▶ Activity  2 · cmd 1 · plan 1", alignCenter},
		{8, lineBubbleTop, "Codex", alignLeft},
		{9, lineBubbleBody, "The parser is in config.go.", alignLeft},
		{12, lineTurnDivider, "TURN 2", alignCenter},
		{14, lineBubbleTop, "You", alignRight},
		{18, lineBubbleTop, "Codex", alignLeft},
	}
	for _, check := range checks {
		got := lines[check.index]
		if got.Kind != check.kind || got.Text != check.text || got.Alignment != check.alignment {
			t.Fatalf("line %d = %#v, want kind=%v text=%q alignment=%v", check.index, got, check.kind, check.text, check.alignment)
		}
	}
	if !lines[6].Actionable || !lines[6].Activity {
		t.Fatalf("activity line is not actionable: %#v", lines[6])
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

	for i, line := range resized {
		if line.Kind == lineBubbleBody && line.Text == "Thanks." {
			m.transcriptCursor = i
			break
		}
	}
	anchor := resized[m.transcriptCursor].Anchor
	next, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	m = next.(model)
	resized = m.currentTranscriptLines()
	if resized[m.transcriptCursor].Anchor != anchor || resized[m.transcriptCursor].Text != "Thanks." {
		t.Fatalf("resize lost message anchor %q: %#v", anchor, resized[m.transcriptCursor])
	}
}

func TestRenderShowsConversationFirstLayout(t *testing.T) {
	m := transcriptTestModel()
	rendered := m.render()
	for _, want := range []string{"▶ Transcript", "TURN 1", "You", "▶ Activity", "Codex", "╭", "╯", "j/k scroll"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("render missing %q:\n%s", want, rendered)
		}
	}

	m.width = 80
	rendered = m.render()
	if !strings.Contains(rendered, "Transcript 2/3") || strings.Contains(rendered, "Conversations 1/3") {
		t.Fatalf("compact transcript view is unclear:\n%s", rendered)
	}
}

func TestAdaptiveDiffLayouts(t *testing.T) {
	m := diffTestModel()
	m.width = 180
	rendered := stripSGR(m.render())
	for _, want := range []string{"Conversations", "Transcript", "Diff · Turn 1", "update · internal/parser.go", "+new"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("three-pane render missing %q:\n%s", want, rendered)
		}
	}
	assertRenderedWidth(t, rendered, 180)

	m.width = 120
	m.focus = focusList
	rendered = stripSGR(m.render())
	if !strings.Contains(rendered, "Conversations") || !strings.Contains(rendered, "Transcript") ||
		strings.Contains(rendered, "Diff · Turn") {
		t.Fatalf("list-focused medium layout is wrong:\n%s", rendered)
	}
	assertRenderedWidth(t, rendered, 120)

	m.focus = focusTranscript
	rendered = stripSGR(m.render())
	if strings.Contains(rendered, "Conversations") || !strings.Contains(rendered, "Transcript") ||
		!strings.Contains(rendered, "Diff · Turn 1") {
		t.Fatalf("transcript-focused medium layout is wrong:\n%s", rendered)
	}
	assertRenderedWidth(t, rendered, 120)

	m.width = 80
	m.focus = focusDiff
	rendered = stripSGR(m.render())
	if !strings.Contains(rendered, "Diff · Turn 1 3/3") || strings.Contains(rendered, "Transcript 2/3") {
		t.Fatalf("compact diff layout is wrong:\n%s", rendered)
	}
	assertRenderedWidth(t, rendered, 80)
}

func TestThreePaneLayoutUsesTheLargerConfiguredBreakpoint(t *testing.T) {
	m := diffTestModel()
	m.cfg.UI.CompactBreakpoint = 200
	m.cfg.UI.ThreePaneBreakpoint = 160
	m.width = 180
	if !m.compactLayout() || m.threePaneLayout() {
		t.Fatalf("width 180 should remain compact with breakpoint 200")
	}
	m.width = 200
	if m.compactLayout() || !m.threePaneLayout() {
		t.Fatalf("width 200 should enter the three-pane layout")
	}
}

func TestFocusCyclesAcrossThreePanes(t *testing.T) {
	m := diffTestModel()
	m.focus = focusList
	for _, want := range []focus{focusTranscript, focusDiff, focusList} {
		next, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
		m = next.(model)
		if m.focus != want {
			t.Fatalf("focus = %v, want %v", m.focus, want)
		}
	}
}

func TestDiffFollowsSelectedTurnAndResetsScroll(t *testing.T) {
	m := diffTestModel()
	if lines := m.currentDiffLines(); len(lines) == 0 || !strings.Contains(lines[0].Text, "internal/parser.go") {
		t.Fatalf("turn 1 diff = %#v", lines)
	}
	m.diffCursor, m.diffOffset, m.diffColumn = 2, 1, 8

	next, _ := m.handleTranscriptKey("]")
	m = next.(model)
	if turn := m.selectedTurn(); turn != 1 {
		t.Fatalf("selected turn = %d, want 1", turn)
	}
	if lines := m.currentDiffLines(); len(lines) != 0 {
		t.Fatalf("turn 2 should not have a diff: %#v", lines)
	}
	if m.diffCursor != 0 || m.diffOffset != 0 || m.diffColumn != 0 {
		t.Fatalf("diff position was not reset: %#v", m)
	}
}

func TestDiffNavigationAndUnicodeClipping(t *testing.T) {
	m := diffTestModel()
	m.width = 80
	m.transcript.Turns[0].Activity[2].FileChanges[0].Diff =
		"@@ -1 +1 @@\n-" + strings.Repeat("old", 40) + "\n+" + strings.Repeat("日本語", 40)
	m.focus = focusDiff

	next, _ := m.handleDiffKey("l")
	m = next.(model)
	if m.diffColumn != 4 {
		t.Fatalf("horizontal offset = %d, want 4", m.diffColumn)
	}
	next, _ = m.handleDiffKey("j")
	m = next.(model)
	if m.diffCursor != 1 {
		t.Fatalf("vertical cursor = %d, want 1", m.diffCursor)
	}
	if got := sliceDisplayColumns("a日本語z", 1, 4); got != "日本" {
		t.Fatalf("full-width slice = %q, want 日本", got)
	}
	rendered := stripSGR(m.renderDiff(80, 20))
	assertRenderedWidth(t, rendered, 80)
}

func TestDiffLineClassification(t *testing.T) {
	tests := []struct {
		line string
		kind diffLineKind
	}{
		{"@@ -1 +1 @@", diffHunk},
		{"+++ b/file.go", diffMetadata},
		{"--- a/file.go", diffMetadata},
		{"+added", diffAdded},
		{"-removed", diffRemoved},
		{" context", diffContext},
	}
	for _, test := range tests {
		if got := classifyDiffLine(test.line); got != test.kind {
			t.Fatalf("classifyDiffLine(%q) = %v, want %v", test.line, got, test.kind)
		}
	}
}

func assertRenderedWidth(t *testing.T, rendered string, width int) {
	t.Helper()
	for _, line := range strings.Split(rendered, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("rendered line width = %d, want <= %d: %q", got, width, line)
		}
	}
}

func TestTranscriptBubbleAlignmentAndMaximumWidth(t *testing.T) {
	m := transcriptTestModel()
	lines := buildTranscriptLines(m.transcript, 40, false)
	var userTop, assistantTop transcriptLine
	for _, line := range lines {
		if line.Kind != lineBubbleTop {
			continue
		}
		switch line.Role {
		case lineUser:
			userTop = line
		case lineAssistant:
			assistantTop = line
		}
	}
	if userTop.BlockWidth > 30 || assistantTop.BlockWidth > 30 {
		t.Fatalf("bubble exceeds 75%% width: user=%d assistant=%d", userTop.BlockWidth, assistantTop.BlockWidth)
	}
	user := stripSGR(m.renderTranscriptLine(userTop, 40))
	assistant := stripSGR(m.renderTranscriptLine(assistantTop, 40))
	if !strings.HasPrefix(assistant, "╭") {
		t.Fatalf("assistant bubble is not left aligned: %q", assistant)
	}
	if strings.HasPrefix(user, "╭") || !strings.HasPrefix(strings.TrimLeft(user, " "), "╭") {
		t.Fatalf("user bubble is not right aligned: %q", user)
	}
	if lipgloss.Width(user) != 40 {
		t.Fatalf("right-aligned user row width = %d, want 40: %q", lipgloss.Width(user), user)
	}
}

func TestTranscriptBubbleWrapsFullWidthTextWithoutOverflow(t *testing.T) {
	m := transcriptTestModel()
	const message = "日本語の長い発言です。全角文字でも安全です。"
	m.transcript.Turns[0].Primary[0].Text = message
	lines := buildTranscriptLines(m.transcript, 24, true)
	var wrapped []string
	for _, line := range lines {
		if line.Kind == lineBubbleBody && line.Role == lineUser && line.Turn == 0 {
			wrapped = append(wrapped, line.Text)
		}
		if got := lipgloss.Width(stripSGR(m.renderTranscriptLine(line, 24))); got > 24 {
			t.Fatalf("rendered line width = %d, want <= 24: %#v", got, line)
		}
	}
	if strings.Join(wrapped, "") != message || len(wrapped) < 2 {
		t.Fatalf("full-width message wrap = %#v", wrapped)
	}
	for _, line := range lines {
		if strings.HasPrefix(line.Anchor, "turn:0:activity:") && line.Alignment != alignCenter {
			t.Fatalf("expanded activity row is not centered: %#v", line)
		}
	}
}

func TestTranscriptBubblePreservesBlankLinesInCompactLayout(t *testing.T) {
	m := transcriptTestModel()
	m.width = 60
	m.transcript.Turns[0].Primary[0].Text = "一行目\n\n二行目"
	lines := m.currentTranscriptLines()
	var body []string
	for _, line := range lines {
		if line.Anchor == "turn:0:message:0:user-1" && line.Kind == lineBubbleBody {
			body = append(body, line.Text)
		}
	}
	if strings.Join(body, "\n") != "一行目\n\n二行目" {
		t.Fatalf("blank lines were not preserved: %#v", body)
	}

	rendered := stripSGR(m.renderTranscript(60, 20))
	for _, line := range strings.Split(rendered, "\n") {
		if width := lipgloss.Width(line); width > 60 {
			t.Fatalf("compact transcript line width = %d, want <= 60: %q", width, line)
		}
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

	m.transcriptView = transcriptConversation
	m.focus = focusDiff
	rendered = m.render()
	if !strings.Contains(rendered, "DIFF") || strings.Contains(rendered, "TRANSCRIPT") {
		t.Fatalf("diff help is not contextual:\n%s", rendered)
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

func diffTestModel() model {
	m := transcriptTestModel()
	m.transcript.Turns[0].Activity = append(m.transcript.Turns[0].Activity, history.Item{
		ID:     "file-1",
		Kind:   "fileChange",
		Title:  "File changes (1)",
		Status: "completed",
		FileChanges: []history.FileChange{{
			Path: "internal/parser.go",
			Kind: "update",
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
		Expandable: true,
	})
	return m
}

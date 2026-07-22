package tui

import (
	"strings"
	"testing"
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

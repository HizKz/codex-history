package config

import (
	"strings"
	"testing"
)

func TestDecodeMergesDefaults(t *testing.T) {
	cfg, err := Decode(strings.NewReader("config_version = 1\n[ui]\nshow_help = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowHelp {
		t.Fatal("expected explicit show_help=false")
	}
	if cfg.Codex.Binary != "codex" || len(cfg.Keys.List.Down) == 0 || len(cfg.Keys.Diff.Right) == 0 ||
		len(cfg.Keys.Activity.Open) == 0 ||
		len(cfg.Keys.Detail.Close) == 0 || len(cfg.Keys.Global.FilterProject) == 0 || len(cfg.Keys.Project.Accept) == 0 {
		t.Fatal("expected unspecified values to retain defaults")
	}
	if cfg.Resume.Mode != "replace" {
		t.Fatalf("resume mode = %q, want replace", cfg.Resume.Mode)
	}
}

func TestThreePaneBreakpointValidation(t *testing.T) {
	cfg := Defaults()
	cfg.UI.ThreePaneBreakpoint = 119
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "three_pane_breakpoint") {
		t.Fatalf("expected three-pane breakpoint error, got %v", err)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	_, err := Decode(strings.NewReader("config_version = 1\nunknown = true\n"))
	if err == nil {
		t.Fatalf("expected an unknown-field error, got %v", err)
	}
}

func TestCanonicalKeyPreservesUppercase(t *testing.T) {
	for _, input := range []string{"R", "shift+r"} {
		got, err := CanonicalKey(input)
		if err != nil {
			t.Fatal(err)
		}
		if got != "R" {
			t.Fatalf("CanonicalKey(%q) = %q, want R", input, got)
		}
	}
}

func TestUseDefaultsFalseAllowsAnEmptyKeymap(t *testing.T) {
	cfg, err := Decode(strings.NewReader("config_version = 1\n[keys]\nuse_defaults = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Keys.Global.Quit) != 0 {
		t.Fatalf("expected an empty custom keymap, got %v", cfg.Keys.Global.Quit)
	}
}

func TestReservedEmergencyKey(t *testing.T) {
	cfg := Defaults()
	cfg.Keys.Global.Quit = []string{"ctrl+c"}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved-key error, got %v", err)
	}
}

func TestActivityKeyConflictsWithGlobalKeys(t *testing.T) {
	cfg := Defaults()
	cfg.Keys.Activity.Open = []string{"q"}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "while activity is focused") {
		t.Fatalf("expected activity-scope conflict, got %v", err)
	}
}

func TestDiffKeyConflictsWithGlobalKeys(t *testing.T) {
	cfg := Defaults()
	cfg.Keys.Diff.Right = []string{"q"}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "while diff is focused") {
		t.Fatalf("expected diff-scope conflict, got %v", err)
	}
}

func TestExplicitKeyOverridesAnInheritedDefault(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`
config_version = 1
[keys.list]
first = ["p"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Keys.Match("list", "first", "p") {
		t.Fatal("expected the explicit list binding to be retained")
	}
	if cfg.Keys.Match("global", "filter_project", "p") {
		t.Fatal("expected the conflicting inherited project binding to be removed")
	}
}

func TestExplicitKeyConflictIsRejected(t *testing.T) {
	_, err := Decode(strings.NewReader(`
config_version = 1
[keys.search]
accept = ["enter"]
cancel = ["enter"]
`))
	if err == nil || !strings.Contains(err.Error(), "while search is focused") {
		t.Fatalf("expected an explicit search-scope conflict, got %v", err)
	}
}

func TestProjectKeyConflictIsRejected(t *testing.T) {
	cfg := Defaults()
	cfg.Keys.Project.Accept = []string{"j"}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "while project is focused") {
		t.Fatalf("expected a project-scope conflict, got %v", err)
	}
}

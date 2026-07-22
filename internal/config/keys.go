package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

type KeyConfig struct {
	UseDefaults bool           `toml:"use_defaults"`
	Global      GlobalKeys     `toml:"global"`
	List        ListKeys       `toml:"list"`
	Transcript  TranscriptKeys `toml:"transcript"`
	Search      SearchKeys     `toml:"search"`
}

type GlobalKeys struct {
	Quit           []string `toml:"quit"`
	Help           []string `toml:"help"`
	FocusNext      []string `toml:"focus_next"`
	Search         []string `toml:"search"`
	Refresh        []string `toml:"refresh"`
	RebuildIndex   []string `toml:"rebuild_index"`
	ReloadConfig   []string `toml:"reload_config"`
	ToggleSources  []string `toml:"toggle_sources"`
	ToggleArchived []string `toml:"toggle_archived"`
}

type ListKeys struct {
	Up       []string `toml:"up"`
	Down     []string `toml:"down"`
	PageUp   []string `toml:"page_up"`
	PageDown []string `toml:"page_down"`
	First    []string `toml:"first"`
	Last     []string `toml:"last"`
	Resume   []string `toml:"resume"`
}

type TranscriptKeys struct {
	Up         []string `toml:"up"`
	Down       []string `toml:"down"`
	PageUp     []string `toml:"page_up"`
	PageDown   []string `toml:"page_down"`
	ToggleItem []string `toml:"toggle_item"`
}

type SearchKeys struct {
	Accept []string `toml:"accept"`
	Cancel []string `toml:"cancel"`
	Clear  []string `toml:"clear"`
}

type Binding struct {
	Scope  string
	Action string
	Keys   []string
}

func (k KeyConfig) Bindings() []Binding {
	return []Binding{
		{"global", "quit", k.Global.Quit},
		{"global", "help", k.Global.Help},
		{"global", "focus_next", k.Global.FocusNext},
		{"global", "search", k.Global.Search},
		{"global", "refresh", k.Global.Refresh},
		{"global", "rebuild_index", k.Global.RebuildIndex},
		{"global", "reload_config", k.Global.ReloadConfig},
		{"global", "toggle_sources", k.Global.ToggleSources},
		{"global", "toggle_archived", k.Global.ToggleArchived},
		{"list", "up", k.List.Up},
		{"list", "down", k.List.Down},
		{"list", "page_up", k.List.PageUp},
		{"list", "page_down", k.List.PageDown},
		{"list", "first", k.List.First},
		{"list", "last", k.List.Last},
		{"list", "resume", k.List.Resume},
		{"transcript", "up", k.Transcript.Up},
		{"transcript", "down", k.Transcript.Down},
		{"transcript", "page_up", k.Transcript.PageUp},
		{"transcript", "page_down", k.Transcript.PageDown},
		{"transcript", "toggle_item", k.Transcript.ToggleItem},
		{"search", "accept", k.Search.Accept},
		{"search", "cancel", k.Search.Cancel},
		{"search", "clear", k.Search.Clear},
	}
}

func (k KeyConfig) KeysFor(scope, action string) []string {
	for _, binding := range k.Bindings() {
		if binding.Scope == scope && binding.Action == action {
			return binding.Keys
		}
	}
	return nil
}

func (k KeyConfig) Match(scope, action, key string) bool {
	canonical, err := CanonicalKey(key)
	if err != nil {
		return false
	}
	for _, candidate := range k.KeysFor(scope, action) {
		parsed, _ := CanonicalKey(candidate)
		if parsed == canonical {
			return true
		}
	}
	return false
}

func ValidateKeys(keys KeyConfig) error {
	var problems []string
	bindings := keys.Bindings()
	for i := range bindings {
		seen := map[string]bool{}
		for _, raw := range bindings[i].Keys {
			canonical, err := CanonicalKey(raw)
			if err != nil {
				problems = append(problems, fmt.Sprintf("keys.%s.%s: %v", bindings[i].Scope, bindings[i].Action, err))
				continue
			}
			if canonical == "ctrl+c" {
				problems = append(problems, fmt.Sprintf("keys.%s.%s: ctrl+c is reserved for emergency exit", bindings[i].Scope, bindings[i].Action))
			}
			if seen[canonical] {
				problems = append(problems, fmt.Sprintf("keys.%s.%s: duplicate key %q", bindings[i].Scope, bindings[i].Action, raw))
			}
			seen[canonical] = true
		}
	}

	for _, activeScope := range []string{"list", "transcript"} {
		owners := map[string]string{}
		for _, binding := range bindings {
			if binding.Scope != "global" && binding.Scope != activeScope {
				continue
			}
			for _, raw := range binding.Keys {
				canonical, err := CanonicalKey(raw)
				if err != nil || canonical == "ctrl+c" {
					continue
				}
				owner := binding.Scope + "." + binding.Action
				if prior, ok := owners[canonical]; ok && prior != owner {
					problems = append(problems, fmt.Sprintf("key %q conflicts between %s and %s while %s is focused", canonical, prior, owner, activeScope))
				} else {
					owners[canonical] = owner
				}
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func CanonicalKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("key must not be empty")
	}
	lower := strings.ToLower(value)
	aliases := map[string]string{
		"escape": "esc", "return": "enter", "pageup": "pgup", "pagedown": "pgdown",
		" ": "space",
	}
	if alias, ok := aliases[lower]; ok {
		lower = alias
	}
	named := []string{"up", "down", "left", "right", "enter", "esc", "tab", "space", "pgup", "pgdown", "home", "end", "backspace", "delete", "insert"}
	if slices.Contains(named, lower) {
		return lower, nil
	}
	if len(lower) >= 2 && lower[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(lower, "f%d", &n); err == nil && n >= 1 && n <= 12 {
			return lower, nil
		}
	}
	parts := strings.Split(lower, "+")
	if len(parts) > 1 {
		if len(parts) != 2 || !slices.Contains([]string{"ctrl", "alt", "shift"}, parts[0]) {
			return "", fmt.Errorf("unsupported key expression %q", value)
		}
		base := parts[1]
		if utf8.RuneCountInString(base) != 1 && !slices.Contains(named, base) {
			return "", fmt.Errorf("modifier must apply to one character or a named key in %q", value)
		}
		if parts[0] == "shift" && utf8.RuneCountInString(base) == 1 {
			return strings.ToUpper(base), nil
		}
		return parts[0] + "+" + base, nil
	}
	if utf8.RuneCountInString(value) == 1 {
		return value, nil
	}
	return "", fmt.Errorf("unsupported key %q", value)
}

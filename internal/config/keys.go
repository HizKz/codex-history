package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

type KeyConfig struct {
	UseDefaults bool           `toml:"use_defaults"`
	Global      GlobalKeys     `toml:"global"`
	List        ListKeys       `toml:"list"`
	Transcript  TranscriptKeys `toml:"transcript"`
	Diff        DiffKeys       `toml:"diff"`
	Activity    ActivityKeys   `toml:"activity"`
	Detail      DetailKeys     `toml:"detail"`
	Search      SearchKeys     `toml:"search"`
	Project     ProjectKeys    `toml:"project"`
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
	FilterProject  []string `toml:"filter_project"`
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
	Up           []string `toml:"up"`
	Down         []string `toml:"down"`
	PageUp       []string `toml:"page_up"`
	PageDown     []string `toml:"page_down"`
	PreviousTurn []string `toml:"previous_turn"`
	NextTurn     []string `toml:"next_turn"`
	ToggleItem   []string `toml:"toggle_item"`
}

type DiffKeys struct {
	Up       []string `toml:"up"`
	Down     []string `toml:"down"`
	PageUp   []string `toml:"page_up"`
	PageDown []string `toml:"page_down"`
	Left     []string `toml:"left"`
	Right    []string `toml:"right"`
}

type ActivityKeys struct {
	Up    []string `toml:"up"`
	Down  []string `toml:"down"`
	Open  []string `toml:"open"`
	Close []string `toml:"close"`
}

type DetailKeys struct {
	Up       []string `toml:"up"`
	Down     []string `toml:"down"`
	PageUp   []string `toml:"page_up"`
	PageDown []string `toml:"page_down"`
	Close    []string `toml:"close"`
}

type SearchKeys struct {
	Accept []string `toml:"accept"`
	Cancel []string `toml:"cancel"`
	Clear  []string `toml:"clear"`
}

type ProjectKeys struct {
	Up     []string `toml:"up"`
	Down   []string `toml:"down"`
	Accept []string `toml:"accept"`
	Cancel []string `toml:"cancel"`
}

type Binding struct {
	Scope  string
	Action string
	Keys   []string
}

func (k KeyConfig) Bindings() []Binding {
	var bindings []Binding
	visitBindingFields(&k, func(scope, action string, keys *[]string) {
		bindings = append(bindings, Binding{Scope: scope, Action: action, Keys: *keys})
	})
	return bindings
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

	for _, activeScope := range []string{"list", "transcript", "diff", "activity", "detail", "search", "project"} {
		owners := map[string]string{}
		for _, binding := range bindings {
			includeGlobal := activeScope != "search" && activeScope != "project"
			if binding.Scope != activeScope && (!includeGlobal || binding.Scope != "global") {
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

func ResolveInheritedKeyConflicts(keys *KeyConfig, raw map[string]any) {
	explicit := explicitKeyBindings(raw)
	if len(explicit) == 0 {
		return
	}
	var claims []Binding
	for _, binding := range keys.Bindings() {
		if explicit[binding.Scope+"."+binding.Action] {
			claims = append(claims, binding)
		}
	}
	visitBindingFields(keys, func(scope, action string, inherited *[]string) {
		if explicit[scope+"."+action] {
			return
		}
		filtered := (*inherited)[:0]
		for _, candidate := range *inherited {
			canonical, err := CanonicalKey(candidate)
			if err != nil || !claimedByExplicit(claims, scope, canonical) {
				filtered = append(filtered, candidate)
			}
		}
		*inherited = filtered
	})
}

func explicitKeyBindings(raw map[string]any) map[string]bool {
	explicit := map[string]bool{}
	keys, ok := raw["keys"].(map[string]any)
	if !ok {
		return explicit
	}
	for scope, value := range keys {
		actions, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for action := range actions {
			explicit[scope+"."+action] = true
		}
	}
	return explicit
}

func claimedByExplicit(claims []Binding, inheritedScope, canonical string) bool {
	for _, claim := range claims {
		if !scopesOverlap(claim.Scope, inheritedScope) {
			continue
		}
		for _, raw := range claim.Keys {
			key, err := CanonicalKey(raw)
			if err == nil && key == canonical {
				return true
			}
		}
	}
	return false
}

func scopesOverlap(a, b string) bool {
	if a == b {
		return true
	}
	if a != "global" && b != "global" {
		return false
	}
	other := a
	if other == "global" {
		other = b
	}
	return other != "search" && other != "project"
}

func visitBindingFields(keys *KeyConfig, visit func(scope, action string, keys *[]string)) {
	root := reflect.ValueOf(keys).Elem()
	rootType := root.Type()
	for i := 0; i < root.NumField(); i++ {
		scope := rootType.Field(i).Tag.Get("toml")
		nested := root.Field(i)
		if nested.Kind() != reflect.Struct {
			continue
		}
		nestedType := nested.Type()
		for j := 0; j < nested.NumField(); j++ {
			field := nested.Field(j)
			if field.Kind() != reflect.Slice || field.Type().Elem().Kind() != reflect.String {
				continue
			}
			action := nestedType.Field(j).Tag.Get("toml")
			binding := field.Addr().Interface().(*[]string)
			visit(scope, action, binding)
		}
	}
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

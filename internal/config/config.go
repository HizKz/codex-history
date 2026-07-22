package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	CurrentVersion = 1
	EnvConfigPath  = "CODEX_HISTORY_CONFIG"
)

//go:embed default.toml
var defaultTemplate []byte

type Config struct {
	ConfigVersion int           `toml:"config_version"`
	Codex         CodexConfig   `toml:"codex"`
	UI            UIConfig      `toml:"ui"`
	History       HistoryConfig `toml:"history"`
	Search        SearchConfig  `toml:"search"`
	Resume        ResumeConfig  `toml:"resume"`
	Keys          KeyConfig     `toml:"keys"`
}

type CodexConfig struct {
	Binary string `toml:"binary"`
}

type UIConfig struct {
	Theme             string      `toml:"theme"`
	ShowHelp          bool        `toml:"show_help"`
	ShowTimestamps    bool        `toml:"show_timestamps"`
	DateFormat        string      `toml:"date_format"`
	ToolDetails       string      `toml:"tool_details"`
	CompactBreakpoint int         `toml:"compact_breakpoint"`
	Colors            ColorConfig `toml:"colors"`
}

type ColorConfig struct {
	Accent    string `toml:"accent"`
	Selected  string `toml:"selected"`
	Muted     string `toml:"muted"`
	Border    string `toml:"border"`
	User      string `toml:"user"`
	Assistant string `toml:"assistant"`
	Warning   string `toml:"warning"`
	Error     string `toml:"error"`
}

type HistoryConfig struct {
	Sources         []string `toml:"sources"`
	IncludeArchived bool     `toml:"include_archived"`
	SortKey         string   `toml:"sort_key"`
	SortDirection   string   `toml:"sort_direction"`
	PageSize        int      `toml:"page_size"`
}

type SearchConfig struct {
	Cache            bool `toml:"cache"`
	IndexOnStartup   bool `toml:"index_on_startup"`
	MaxParallelReads int  `toml:"max_parallel_reads"`
}

type ResumeConfig struct {
	Mode string `toml:"mode"`
}

type LoadOptions struct {
	ExplicitPath string
	NoConfig     bool
}

type Loaded struct {
	Config Config
	Path   string
	Found  bool
}

func Defaults() Config {
	var cfg Config
	if err := toml.Unmarshal(defaultTemplate, &cfg); err != nil {
		panic(fmt.Sprintf("embedded default config is invalid: %v", err))
	}
	return cfg
}

func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(base, "codex-history", "config.toml"), nil
}

func ResolvePath(explicit string) (path string, required bool, err error) {
	if explicit != "" {
		return explicit, true, nil
	}
	if env := os.Getenv(EnvConfigPath); env != "" {
		return env, true, nil
	}
	path, err = DefaultPath()
	return path, false, err
}

func Load(opts LoadOptions) (Loaded, error) {
	path, required, err := ResolvePath(opts.ExplicitPath)
	if err != nil {
		return Loaded{}, err
	}
	if opts.NoConfig {
		cfg := Defaults()
		return Loaded{Config: cfg, Path: path}, Validate(cfg)
	}

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		cfg := Defaults()
		return Loaded{Config: cfg, Path: path}, Validate(cfg)
	}
	if err != nil {
		return Loaded{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	cfg, err := Decode(f)
	if err != nil {
		return Loaded{}, fmt.Errorf("config %s: %w", path, err)
	}
	return Loaded{Config: cfg, Path: path, Found: true}, nil
}

func Decode(r io.Reader) (Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Config{}, fmt.Errorf("read: %w", err)
	}

	cfg := Defaults()
	var probe map[string]any
	if err := toml.Unmarshal(data, &probe); err != nil {
		return Config{}, err
	}
	if keys, ok := probe["keys"].(map[string]any); ok {
		if useDefaults, ok := keys["use_defaults"].(bool); ok && !useDefaults {
			cfg.Keys = KeyConfig{UseDefaults: false}
		}
	}

	decoder := toml.NewDecoder(strings.NewReader(string(data))).DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Validate(cfg Config) error {
	var problems []string
	if cfg.ConfigVersion != CurrentVersion {
		problems = append(problems, fmt.Sprintf("config_version must be %d (got %d)", CurrentVersion, cfg.ConfigVersion))
	}
	if strings.TrimSpace(cfg.Codex.Binary) == "" {
		problems = append(problems, "codex.binary must not be empty")
	}
	if !slices.Contains([]string{"terminal", "dark", "light"}, cfg.UI.Theme) {
		problems = append(problems, "ui.theme must be terminal, dark, or light")
	}
	if !slices.Contains([]string{"collapsed", "expanded"}, cfg.UI.ToolDetails) {
		problems = append(problems, "ui.tool_details must be collapsed or expanded")
	}
	if cfg.UI.CompactBreakpoint < 40 {
		problems = append(problems, "ui.compact_breakpoint must be at least 40")
	}
	for name, value := range map[string]string{
		"accent": cfg.UI.Colors.Accent, "selected": cfg.UI.Colors.Selected,
		"muted": cfg.UI.Colors.Muted, "border": cfg.UI.Colors.Border,
		"user": cfg.UI.Colors.User, "assistant": cfg.UI.Colors.Assistant,
		"warning": cfg.UI.Colors.Warning, "error": cfg.UI.Colors.Error,
	} {
		if !validColor(value) {
			problems = append(problems, fmt.Sprintf("ui.colors.%s has invalid color %q", name, value))
		}
	}
	allowedSources := []string{"cli", "vscode", "app_server", "exec", "sub_agent", "unknown"}
	for _, source := range cfg.History.Sources {
		if !slices.Contains(allowedSources, source) {
			problems = append(problems, fmt.Sprintf("history.sources contains unsupported source %q", source))
		}
	}
	if len(cfg.History.Sources) == 0 {
		problems = append(problems, "history.sources must not be empty")
	}
	if !slices.Contains([]string{"created", "updated", "recency"}, cfg.History.SortKey) {
		problems = append(problems, "history.sort_key must be created, updated, or recency")
	}
	if !slices.Contains([]string{"asc", "desc"}, cfg.History.SortDirection) {
		problems = append(problems, "history.sort_direction must be asc or desc")
	}
	if cfg.History.PageSize < 1 || cfg.History.PageSize > 500 {
		problems = append(problems, "history.page_size must be between 1 and 500")
	}
	if cfg.Search.MaxParallelReads < 1 || cfg.Search.MaxParallelReads > 32 {
		problems = append(problems, "search.max_parallel_reads must be between 1 and 32")
	}
	if !slices.Contains([]string{"return", "replace", "print_command"}, cfg.Resume.Mode) {
		problems = append(problems, "resume.mode must be return, replace, or print_command")
	}
	if err := ValidateKeys(cfg.Keys); err != nil {
		problems = append(problems, err.Error())
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func validColor(value string) bool {
	if value == "default" {
		return true
	}
	named := []string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white", "bright_black", "bright_red", "bright_green", "bright_yellow", "bright_blue", "bright_magenta", "bright_cyan", "bright_white"}
	if slices.Contains(named, value) {
		return true
	}
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func Template() []byte {
	return append([]byte(nil), defaultTemplate...)
}

func Init(path string, force bool) error {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config already exists at %s (use --force to replace it)", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(defaultTemplate); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func Marshal(cfg Config) ([]byte, error) {
	return toml.Marshal(cfg)
}

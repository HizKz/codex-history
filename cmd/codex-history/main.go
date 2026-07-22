package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/HizKz/codex-history/internal/appserver"
	"github.com/HizKz/codex-history/internal/buildinfo"
	"github.com/HizKz/codex-history/internal/config"
	"github.com/HizKz/codex-history/internal/index"
	"github.com/HizKz/codex-history/internal/tui"
)

const usage = `codex-history — browse, search, and resume local Codex conversations

Usage:
  codex-history [options]
  codex-history config path|init|check|show [options]
  codex-history doctor [--json] [options]

Options:
  --config PATH      use this TOML configuration file
  --no-config        ignore configuration files
  --codex-bin PATH   override the Codex executable
  --cache PATH       override the SQLite search index path
  --no-cache         keep the search index in memory
  --reindex          rebuild the search index at startup
  --version          print version information
  --help             show this help

Run "codex-history config init" to create an editable configuration file.
`

type rootOptions struct {
	configPath string
	noConfig   bool
	codexBin   string
	cachePath  string
	noCache    bool
	reindex    bool
	version    bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "codex-history:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, usage)
		return nil
	}
	if len(args) > 0 {
		switch args[0] {
		case "config":
			return runConfig(args[1:], stdout, stderr)
		case "doctor":
			return runDoctor(args[1:], stdout, stderr)
		case "help":
			fmt.Fprint(stdout, usage)
			return nil
		}
	}
	opts, err := parseRoot(args, stderr)
	if err != nil {
		return err
	}
	if opts.version {
		fmt.Fprintln(stdout, buildinfo.String())
		return nil
	}
	loadOpts := config.LoadOptions{ExplicitPath: opts.configPath, NoConfig: opts.noConfig}
	loaded, err := config.Load(loadOpts)
	if err != nil {
		return err
	}
	if opts.codexBin != "" {
		loaded.Config.Codex.Binary = opts.codexBin
	}
	persistent := loaded.Config.Search.Cache && !opts.noCache
	store, err := index.Open(opts.cachePath, persistent)
	if err != nil {
		return fmt.Errorf("open search index: %w", err)
	}
	defer store.Close()
	if opts.reindex {
		if err := store.Clear(context.Background()); err != nil {
			return fmt.Errorf("clear search index: %w", err)
		}
		loaded.Config.Search.IndexOnStartup = true
	}
	outcome, err := tui.Run(context.Background(), tui.Options{
		Config: loaded.Config, ConfigPath: loaded.Path, LoadOptions: loadOpts, Index: store,
	})
	if err != nil {
		return err
	}
	if outcome.PrintedCommand != "" {
		fmt.Fprintln(stdout, outcome.PrintedCommand)
	}
	return nil
}

func parseRoot(args []string, stderr io.Writer) (rootOptions, error) {
	var opts rootOptions
	fs := flag.NewFlagSet("codex-history", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.configPath, "config", "", "configuration file")
	fs.BoolVar(&opts.noConfig, "no-config", false, "ignore configuration files")
	fs.StringVar(&opts.codexBin, "codex-bin", "", "Codex executable")
	fs.StringVar(&opts.cachePath, "cache", "", "search index path")
	fs.BoolVar(&opts.noCache, "no-cache", false, "use an in-memory search index")
	fs.BoolVar(&opts.reindex, "reindex", false, "rebuild the search index")
	fs.BoolVar(&opts.version, "version", false, "show version")
	help := fs.Bool("help", false, "show help")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if len(fs.Args()) > 0 {
		return opts, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *help {
		fmt.Fprint(stderr, usage)
		return opts, flag.ErrHelp
	}
	return opts, nil
}

func runConfig(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("config requires one of: path, init, check, show")
	}
	action := args[0]
	fs := flag.NewFlagSet("codex-history config "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var path string
	var force bool
	fs.StringVar(&path, "config", "", "configuration file")
	fs.StringVar(&path, "path", "", "configuration file")
	fs.BoolVar(&force, "force", false, "replace an existing file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if len(fs.Args()) > 1 || (len(fs.Args()) == 1 && path != "") {
		return errors.New("provide the config path either positionally or with --config")
	}
	if len(fs.Args()) == 1 {
		path = fs.Args()[0]
	}
	resolved, _, err := config.ResolvePath(path)
	if err != nil {
		return err
	}
	switch action {
	case "path":
		fmt.Fprintln(stdout, resolved)
		return nil
	case "init":
		if err := config.Init(resolved, force); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Created", resolved)
		return nil
	case "check":
		loaded, err := config.Load(config.LoadOptions{ExplicitPath: resolved})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "OK: %s (config_version=%d)\n", loaded.Path, loaded.Config.ConfigVersion)
		return nil
	case "show":
		loaded, err := config.Load(config.LoadOptions{ExplicitPath: resolved})
		if err != nil {
			return err
		}
		data, err := config.Marshal(loaded.Config)
		if err != nil {
			return err
		}
		_, err = stdout.Write(data)
		return err
	default:
		return fmt.Errorf("unknown config action %q", action)
	}
}

type doctorReport struct {
	OK             bool   `json:"ok"`
	Version        string `json:"version"`
	ConfigPath     string `json:"config_path"`
	ConfigFound    bool   `json:"config_found"`
	CodexBinary    string `json:"codex_binary"`
	CodexVersion   string `json:"codex_version,omitempty"`
	AppServer      bool   `json:"app_server"`
	ThreadRead     bool   `json:"thread_read"`
	ConversationNo int    `json:"conversation_count"`
	CachePath      string `json:"cache_path"`
	Error          string `json:"error,omitempty"`
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("codex-history doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var path, codexBin, cachePath string
	var jsonOutput, noConfig, noCache bool
	fs.StringVar(&path, "config", "", "configuration file")
	fs.StringVar(&codexBin, "codex-bin", "", "Codex executable")
	fs.StringVar(&cachePath, "cache", "", "search index path")
	fs.BoolVar(&jsonOutput, "json", false, "emit JSON")
	fs.BoolVar(&noConfig, "no-config", false, "ignore configuration files")
	fs.BoolVar(&noCache, "no-cache", false, "use an in-memory search index")
	if err := fs.Parse(args); err != nil {
		return err
	}
	loaded, err := config.Load(config.LoadOptions{ExplicitPath: path, NoConfig: noConfig})
	if err != nil {
		return err
	}
	if codexBin != "" {
		loaded.Config.Codex.Binary = codexBin
	}
	store, err := index.Open(cachePath, loaded.Config.Search.Cache && !noCache)
	if err != nil {
		return err
	}
	defer store.Close()
	report := doctorReport{
		Version: buildinfo.String(), ConfigPath: loaded.Path, ConfigFound: loaded.Found,
		CodexBinary: loaded.Config.Codex.Binary, CachePath: store.Path(),
	}
	if output, cmdErr := exec.Command(loaded.Config.Codex.Binary, "--version").CombinedOutput(); cmdErr != nil {
		report.Error = cmdErr.Error()
	} else {
		report.CodexVersion = strings.TrimSpace(string(output))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if report.Error == "" {
		client, startErr := appserver.Start(ctx, loaded.Config.Codex.Binary)
		if startErr != nil {
			report.Error = startErr.Error()
		} else {
			report.AppServer = true
			threads, listErr := client.ListThreads(ctx, appserver.ListOptions{
				SourceKinds: appserver.SourceKinds(loaded.Config.History.Sources),
				Limit:       1, SortKey: loaded.Config.History.SortKey,
				SortDirection: loaded.Config.History.SortDirection,
			})
			if listErr != nil {
				report.Error = listErr.Error()
			} else {
				report.ConversationNo = len(threads)
				if len(threads) > 0 {
					_, readErr := client.ReadThread(ctx, threads[0].ID)
					if readErr != nil {
						report.Error = readErr.Error()
					} else {
						report.ThreadRead = true
					}
				}
			}
			_ = client.Close()
		}
	}
	report.OK = report.Error == ""
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Fprintln(stdout, tui.ConfigSummary(loaded, report.CachePath))
	fmt.Fprintln(stdout, "version:", report.Version)
	fmt.Fprintln(stdout, "codex:  ", report.CodexVersion)
	fmt.Fprintf(stdout, "app-server: %t (read: %t, %d conversations visible)\n", report.AppServer, report.ThreadRead, report.ConversationNo)
	if report.Error != "" {
		return errors.New(report.Error)
	}
	return nil
}

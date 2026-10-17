package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"mirr/internal/auth"
	"mirr/internal/config"
	"mirr/internal/engine"
	"mirr/internal/history"
	"mirr/internal/terminal"
	"mirr/internal/ui"
)

var errOperationFailed = errors.New("one or more mirrors failed")

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__askpass" {
		if len(os.Args) > 2 && strings.Contains(strings.ToLower(os.Args[2]), "username") {
			fmt.Println(os.Getenv("MIRR_AUTH_USER"))
		} else {
			fmt.Println(os.Getenv("MIRR_AUTH_SECRET"))
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "mirr:", terminal.Safe(err.Error()))
		if errors.Is(err, errOperationFailed) {
			os.Exit(1)
		}
		os.Exit(2)
	}
}

func root() *cobra.Command {
	var file, output, to string
	var all, nonInteractive bool
	var timeout time.Duration
	load := func() (*config.Config, error) { return config.Load(file) }
	save := func(rs []engine.Result) error { return history.Save(file, rs) }
	readHistory := func() ([]engine.Result, error) { return history.Load(file) }
	tui := func(cmd *cobra.Command, args []string) error {
		if nonInteractive {
			return fmt.Errorf("TUI is unavailable with --non-interactive")
		}
		if _, err := load(); err != nil {
			return fmt.Errorf("%w; run mirr init --help to create a config", err)
		}
		return ui.Run(cmd.Context(), load, readHistory, save, func(project, mirror, mode string) error { return config.SetMode(file, project, mirror, mode) })
	}
	r := &cobra.Command{Use: "mirr", Short: "One source. Many mirrors.", Long: "mirr — inspect and synchronize Git mirrors from your terminal.", SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs, RunE: tui}
	r.PersistentFlags().StringVarP(&file, "config", "c", ".mirr.yaml", "YAML config path")
	r.PersistentFlags().StringVar(&output, "output", "text", "Output format: text or json")
	r.PersistentFlags().BoolVar(&nonInteractive, "non-interactive", false, "Disable interactive UI and prompts")
	r.PersistentFlags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for CLI Git operations")
	r.AddCommand(&cobra.Command{Use: "tui", Aliases: []string{"ui"}, Short: "Open the mirror dashboard", Args: cobra.NoArgs, RunE: tui})
	r.AddCommand(&cobra.Command{Use: "validate", Aliases: []string{"check"}, Short: "Validate config without contacting remotes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkOutput(output); err != nil {
			return err
		}
		_, e := load()
		if e == nil {
			if output == "json" {
				fmt.Println(`{"valid":true}`)
			} else {
				fmt.Println("Config is valid.")
			}
		}
		return e
	}})
	for _, verb := range []string{"plan", "status", "branches", "tags", "sync"} {
		verb := verb
		aliases := map[string][]string{"plan": {"p"}, "status": {"st"}, "branches": {"b"}, "tags": {"t"}, "sync": {"s"}}
		c := &cobra.Command{Use: verb + " [repository]", Aliases: aliases[verb], Short: map[string]string{"plan": "Preview Git changes", "status": "Inspect current remote state", "branches": "Compare source and mirror branches", "tags": "Compare source and mirror tags", "sync": "Synchronize configured mirrors"}[verb], Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkOutput(output); err != nil {
				return err
			}
			cfg, e := load()
			if e != nil {
				return e
			}
			project := ""
			if len(args) > 0 {
				project = args[0]
				if all {
					return fmt.Errorf("choose a repository or --all")
				}
				if _, ok := cfg.Repositories[project]; !ok {
					return fmt.Errorf("unknown repository %q", project)
				}
			} else if !all && verb == "sync" {
				if len(cfg.Repositories) == 1 {
					for name := range cfg.Repositories {
						project = name
					}
				} else {
					return fmt.Errorf("sync needs a repository name; choose one with 'mirr sync NAME' or use 'mirr sync --all'")
				}
			}
			if verb == "sync" {
				if _, err := readHistory(); err != nil {
					return err
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			rs := (&engine.Engine{Config: cfg}).Run(ctx, project, to, verb == "sync")
			if len(rs) == 0 {
				return fmt.Errorf("no matching mirrors")
			}
			var saveErr error
			if verb == "sync" {
				saveErr = save(rs)
			}
			if output == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if e = enc.Encode(rs); e != nil {
					return e
				}
			} else if verb == "branches" || verb == "tags" {
				printRefs(rs, verb)
			} else {
				printResults(rs)
			}
			if saveErr != nil && engine.Failed(rs) {
				fmt.Fprintln(os.Stderr, "Could not save sync history:", terminal.Safe(saveErr.Error()))
			}
			if engine.Failed(rs) {
				return errOperationFailed
			}
			if saveErr != nil {
				return fmt.Errorf("sync completed but history could not be saved: %w", saveErr)
			}
			return nil
		}}
		c.Flags().BoolVar(&all, "all", false, "Select all repositories")
		c.Flags().StringVar(&to, "to", "", "Select a mirror by name")
		r.AddCommand(c)
	}
	r.AddCommand(&cobra.Command{Use: "history", Short: "Show last recorded sync per mirror", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkOutput(output); err != nil {
			return err
		}
		rs, e := history.Load(file)
		if e != nil {
			return e
		}
		if len(rs) == 0 {
			if output == "json" {
				fmt.Println("[]")
			} else {
				fmt.Println("No sync history yet.")
			}
			return nil
		}
		if output == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rs)
		} else {
			printResults(rs)
		}
		return nil
	}})
	var source, mirror, name, mode string
	initCmd := &cobra.Command{Use: "init", Short: "Create a config (never overwrites an existing file)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if source == "" && mirror == "" && isTerminal(os.Stdin) {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			defaultName := filepath.Base(cwd)
			if defaultName == "." || defaultName == string(filepath.Separator) || defaultName == "" {
				defaultName = "my-project"
			}
			reader := bufio.NewReader(os.Stdin)
			source, err = prompt(reader, "Source Git URL or local path", ".")
			if err != nil {
				return err
			}
			mirror, err = prompt(reader, "Mirror Git URL or local path", "")
			if err != nil {
				return err
			}
			name, err = prompt(reader, "Repository name", defaultName)
			if err != nil {
				return err
			}
			mode, err = prompt(reader, "Mode (release or continuous)", "release")
			if err != nil {
				return err
			}
		}
		if source == "" || mirror == "" {
			return fmt.Errorf("use mirr init --source URL --mirror URL [--name project] [--mode release|continuous]")
		}
		if mode != "release" && mode != "continuous" {
			return fmt.Errorf("invalid mode")
		}
		cfg := config.Config{Version: 1, Repositories: map[string]config.Repository{name: {Source: config.Endpoint{URL: source}, Defaults: config.Settings{Mode: mode, Tags: &config.Tags{Include: []string{"v*"}, Exclude: []string{"*-rc*", "*-beta*", "*-alpha*"}}, Policy: &config.Policy{}}, Mirrors: map[string]config.Mirror{"origin": {Endpoint: config.Endpoint{URL: mirror}}}}}}
		if err := cfg.Validate(file); err != nil {
			return err
		}
		b, e := yaml.Marshal(cfg)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		fmt.Println("Created", terminal.Safe(file), "— edit mirror names and credentials, then run mirr validate.")
		return nil
	}}
	initCmd.Flags().StringVar(&source, "source", "", "Source Git URL or local path")
	initCmd.Flags().StringVar(&mirror, "mirror", "", "Destination Git URL or local path")
	initCmd.Flags().StringVar(&name, "name", "my-project", "Repository name")
	initCmd.Flags().StringVar(&mode, "mode", "release", "release or continuous")
	r.AddCommand(initCmd)
	authCmd := &cobra.Command{Use: "auth", Short: "Manage HTTPS credentials in the Linux system keyring"}
	authCmd.AddCommand(&cobra.Command{Use: "set PROFILE", Short: "Store a token with hidden input", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) (runErr error) {
		if nonInteractive {
			return fmt.Errorf("use an env credential provider in CI")
		}
		cfg, e := load()
		if e != nil {
			return e
		}
		cr, ok := cfg.Credentials[args[0]]
		if !ok || cr.Provider != "keyring" {
			return fmt.Errorf("profile must exist with provider: keyring")
		}
		get := exec.Command("stty", "-g")
		get.Stdin = os.Stdin
		state, e := get.Output()
		if e != nil {
			return fmt.Errorf("hidden token input requires a terminal")
		}
		set := func(s string) error { c := exec.Command("stty", s); c.Stdin = os.Stdin; return c.Run() }
		if e = set("-echo"); e != nil {
			return e
		}
		defer func() {
			if err := set(strings.TrimSpace(string(state))); err != nil && runErr == nil {
				runErr = fmt.Errorf("could not restore terminal settings: %w", err)
			}
		}()
		fmt.Fprint(os.Stderr, "Token: ")
		secret, e := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Fprintln(os.Stderr)
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		if e = auth.Store(ctx, cr.Account, strings.TrimRight(secret, "\r\n")); e != nil {
			return e
		}
		fmt.Println("Stored in system keyring.")
		return nil
	}})
	r.AddCommand(authCmd)
	return r
}

func checkOutput(output string) error {
	if output != "text" && output != "json" {
		return fmt.Errorf("output must be text or json")
	}
	return nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func prompt(reader *bufio.Reader, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	} else {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, defaultValue)
	}
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}

func printResults(rs []engine.Result) {
	for _, r := range rs {
		fmt.Printf("%s / %s  [%s]  %s  %s\n", terminal.Safe(r.Repository), terminal.Safe(r.Mirror), terminal.Safe(r.Mode), terminal.Safe(r.Status), r.CheckedAt.Format(time.RFC3339))
		for _, c := range r.Changes {
			fmt.Printf("  %-9s %s\n", terminal.Safe(c.Action), terminal.Safe(c.Ref))
		}
		if r.Error != "" {
			fmt.Println("  " + terminal.Safe(r.Error))
		}
	}
}

func printRefs(rs []engine.Result, kind string) {
	for _, r := range rs {
		fmt.Printf("%s / %s  [%s]  %s\n", terminal.Safe(r.Repository), terminal.Safe(r.Mirror), terminal.Safe(r.Mode), terminal.Safe(r.Status))
		if r.Error != "" {
			fmt.Println("  " + terminal.Safe(r.Error))
		}
		refs := r.Branches
		if kind == "tags" {
			refs = r.Tags
		}
		if len(refs) == 0 && r.Error == "" {
			fmt.Printf("  No %s.\n", kind)
		}
		if len(refs) > 0 {
			label := "BRANCH"
			if kind == "tags" {
				label = "TAG"
			}
			fmt.Printf("  %-32s  SOURCE -> MIRROR   STATE\n", label)
		}
		for _, ref := range refs {
			fmt.Printf("  %-32s  %8s -> %8s  %s\n", terminal.Safe(ref.Name), shortSHA(ref.Source), shortSHA(ref.Mirror), ref.State)
		}
		if kind == "tags" && r.Mode == "release" {
			fmt.Println("  Release tag filters apply; use mirr plan to see which tags will be sent.")
		}
	}
}

func shortSHA(value string) string {
	if value == "" {
		return "-"
	}
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

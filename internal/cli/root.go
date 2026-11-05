// Package cli defines the cobra commands. Commands only parse flags and call
// functions in other internal packages; they hold no business logic.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/term"
)

// Environment variables. openarch sets the first three from its global flags
// when it runs this binary as the 'openarch docs' plugin.
const (
	envOutput         = "OPENARCH_OUTPUT"
	envNonInteractive = "OPENARCH_NON_INTERACTIVE"
	envVerbose        = "OPENARCH_VERBOSE"
	envCase           = "OPENARCH_DOCS_CASE"
	envProfile        = "OPENARCH_DOCS_PROFILE"
	envModelURL       = "OPENARCH_DOCS_MODEL_URL"
)

// Streams holds the standard streams used by commands.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// GlobalOptions holds the flags shared by every command.
type GlobalOptions struct {
	Output         string
	Case           string
	Profile        string
	ModelURL       string
	NonInteractive bool
	Verbose        bool
	NoColor        bool
}

// App is the state shared by commands during one execution.
type App struct {
	Opts    GlobalOptions
	Streams Streams
	Printer *output.Printer
	Log     *slog.Logger
}

func newApp(s Streams) *App {
	return &App{Streams: s}
}

// Interactive reports whether commands may prompt the user.
func (a *App) Interactive() bool {
	return !a.Opts.NonInteractive && term.IsTerminal(a.Streams.In) && term.IsTerminal(a.Streams.Out)
}

// Color reports whether text output may use colors.
func (a *App) Color() bool {
	return !a.Opts.NoColor && os.Getenv("NO_COLOR") == "" && term.IsTerminal(a.Streams.Out)
}

func (a *App) setup() error {
	format, err := output.ParseFormat(a.Opts.Output)
	if err != nil {
		return err
	}
	a.Printer = output.NewPrinter(format, a.Streams.Out, a.Streams.Err)
	level := slog.LevelWarn
	if a.Opts.Verbose {
		level = slog.LevelDebug
	}
	a.Log = slog.New(slog.NewTextHandler(a.Streams.Err, &slog.HandlerOptions{Level: level}))
	return nil
}

// Execute runs the CLI with args and returns the process exit code.
func Execute(ctx context.Context, args []string, s Streams) int {
	app := newApp(s)
	root := newRootCmd(app)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return int(output.ExitOK)
	}
	p := app.Printer
	if p == nil {
		// The error happened before flags were fully parsed.
		p = output.NewPrinter(formatFromArgs(args), s.Out, s.Err)
	}
	p.Error(err)
	return int(output.CodeOf(err))
}

// caseRoot returns the absolute path of the case folder.
func (a *App) caseRoot() (string, error) {
	dir := a.Opts.Case
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", output.Usage(fmt.Sprintf("case folder %s is not a folder", abs), "Use --case <folder> or run the command in the case folder.")
	}
	return abs, nil
}

// requireManifest fails with a hint when the case folder has no manifest yet.
func requireManifest(root string) error {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(casefile.ManifestFile))); err != nil {
		return output.Usage("the case folder has no "+casefile.ManifestFile, "Run 'openarch docs ingest' first.")
	}
	return nil
}

func newRootCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "openarch-docs",
		Short: "Work with a folder of case documents, with local models and OpenArch",
		Long: `openarch-docs ingests a folder of case documents (emails, chat exports, PDFs,
scans, images) into OpenArch, extracts their text, and answers questions about
them with local models on Docker Model Runner. It never sends data to the cloud.

It runs as the 'openarch docs' plugin, or on its own as 'openarch-docs'.

Every command supports --output json. Results go to stdout and logs go to stderr.
Exit codes: 0 ok, 1 error, 2 bad usage, 3 prerequisite missing,
4 service not healthy, 5 remote API error, 6 authentication failed.`,
		Example: `  openarch docs version
  openarch-docs version --output json`,
		Args:          noArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return app.setup()
		},
	}
	root.SetIn(app.Streams.In)
	root.SetOut(app.Streams.Out)
	root.SetErr(app.Streams.Err)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &output.Error{Code: output.ExitUsage, Message: err.Error(), Hint: "Run 'openarch-docs --help' for usage.", Err: err}
	})

	pf := root.PersistentFlags()
	pf.StringVarP(&app.Opts.Output, "output", "o", envOr(envOutput, string(output.Text)), "output format: text or json (env "+envOutput+")")
	pf.StringVar(&app.Opts.Case, "case", os.Getenv(envCase), "case folder (env "+envCase+", default: current directory)")
	pf.StringVar(&app.Opts.Profile, "profile", envOr(envProfile, "small"), "model profile: small (16 GB RAM) or large (32 GB) (env "+envProfile+")")
	pf.StringVar(&app.Opts.ModelURL, "model-url", envOr(envModelURL, models.DefaultURL), "Docker Model Runner API (env "+envModelURL+")")
	pf.BoolVar(&app.Opts.NonInteractive, "non-interactive", envBool(envNonInteractive), "never prompt; fail when a value is missing (env "+envNonInteractive+")")
	pf.BoolVarP(&app.Opts.Verbose, "verbose", "v", envBool(envVerbose), "print debug logs to stderr (env "+envVerbose+")")
	pf.BoolVar(&app.Opts.NoColor, "no-color", false, "disable colors (env NO_COLOR)")

	_ = root.RegisterFlagCompletionFunc("profile", cobra.FixedCompletions(models.ProfileNames(), cobra.ShellCompDirectiveNoFileComp))
	root.AddCommand(newIngestCmd(app), newExtractCmd(app), newIndexCmd(app), newSearchCmd(app), newAskCmd(app), newTimelineCmd(app), newVerifyCmd(app), newVersionCmd(app))
	return root
}

// noArgs rejects positional arguments with a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if cmd.HasAvailableSubCommands() {
		hint := fmt.Sprintf("Run '%s --help' for the list of commands.", cmd.CommandPath())
		if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
			hint = fmt.Sprintf("Did you mean %q?", s[0])
		}
		return output.Usage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()), hint)
	}
	return output.Usage(fmt.Sprintf("%q accepts no arguments", cmd.CommandPath()), "")
}

// formatFromArgs finds the output format without cobra, for errors raised
// before flag parsing finished.
func formatFromArgs(args []string) output.Format {
	value := os.Getenv(envOutput)
	for i := 0; i < len(args) && args[i] != "--"; i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--output":
			if i+1 < len(args) {
				value = args[i+1]
			}
		case strings.HasPrefix(a, "--output="):
			value = strings.TrimPrefix(a, "--output=")
		case strings.HasPrefix(a, "-o") && len(a) > 2:
			value = strings.TrimPrefix(strings.TrimPrefix(a, "-o"), "=")
		}
	}
	if output.Format(value) == output.JSON {
		return output.JSON
	}
	return output.Text
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool reports whether key is set to 1 or true.
func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "1" || v == "true"
}

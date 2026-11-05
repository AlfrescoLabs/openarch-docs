package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/caseops"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
)

type initResult struct{ *caseops.InitResult }

func (r initResult) WriteText(w io.Writer) error {
	for _, c := range r.Created {
		if _, err := fmt.Fprintf(w, "created  %s\n", c); err != nil {
			return err
		}
	}
	for _, k := range r.Kept {
		if _, err := fmt.Fprintf(w, "kept     %s\n", k); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Case folder ready: %s. Put the documents in it, then run 'openarch docs ingest'.\n", r.Root)
	return err
}

func newInitCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "init [folder]",
		Short: "Make the layout of a case folder",
		Long: `Make a case folder, or add the layout to one: references/ (laws and other
reference texts), pasted/ (text added with 'add --text'), notes/ and
notes/drafts/. It writes AGENTS.md with the rules for people and agents, and
installs the openarch-docs Agent Skill. It never changes a file that exists.`,
		Example: `  openarch docs init ~/cases/garage
  openarch docs init`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			dir := app.Opts.Case
			if len(args) == 1 {
				dir = args[0]
			}
			if dir == "" {
				dir = "."
			}
			root, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			res, err := caseops.Init(root)
			if err != nil {
				return err
			}
			return app.Printer.Print(initResult{res})
		},
	}
}

type addResult struct{ *caseops.AddResult }

func (r addResult) WriteText(w io.Writer) error {
	for _, f := range r.Files {
		if _, err := fmt.Fprintf(w, "%-8s %s (%s)\n", f.Status, f.Path, f.How); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Ingested, %d derived files. Run extract and index next, or status.\n", r.Derived)
	return err
}

func newAddCmd(app *App) *cobra.Command {
	var (
		text                 bool
		name, dir, sourceURL string
		note                 string
		reference            bool
	)
	cmd := &cobra.Command{
		Use:   "add [file]...",
		Short: "Add files or pasted text to the case folder, with where they came from",
		Long: `Copy files into the case folder, or, with --text, save text from stdin (such
as an email pasted from a web mail) in pasted/. Then ingest them.

The manifest records how each file arrived: copied from a path, or pasted,
with --source-url (where you got it, such as the page of a law) and --note.
With --reference the files go to references/: laws and rules, cited apart
from the facts of the case. The plugin never downloads anything; add here
what you downloaded yourself.

A file with the same name and other content is an error: originals are
never replaced.`,
		Example: `  openarch docs add ~/Downloads/respuesta.eml
  openarch docs add --reference ~/Downloads/BOE-A-1960-10906.pdf --source-url https://www.boe.es/eli/es/l/1960/07/21/49/con
  pbpaste | openarch docs add --text --name 2026-11-04-ultima-actualizacion.txt --note "pasted from Gmail"`,
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			opts := caseops.AddOptions{Root: root, Files: args, Name: name, Dir: dir, SourceURL: sourceURL, Note: note}
			if reference {
				if dir != "" {
					return output.Usage("use --reference or --dir, not both", "")
				}
				opts.Dir = casefile.ReferencesDir
			}
			switch {
			case text && len(args) > 0:
				return output.Usage("--text reads stdin; give no files with it", "")
			case text:
				data, err := io.ReadAll(io.LimitReader(app.Streams.In, 64<<20))
				if err != nil {
					return err
				}
				opts.Text = string(data)
			case len(args) == 0:
				return output.Usage("nothing to add", "Give files, or --text with the text on stdin.")
			}
			res, err := caseops.Add(opts)
			if errors.Is(err, caseops.ErrInCase) {
				return output.Usage(err.Error(), "Run 'openarch docs ingest'.")
			}
			if err != nil {
				return output.Usage(err.Error(), "")
			}
			return app.Printer.Print(addResult{res})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&text, "text", false, "read the text to add from stdin, into pasted/")
	f.StringVar(&name, "name", "", "file name for --text (default: <date>-pasted.txt)")
	f.StringVar(&dir, "dir", "", "folder of the case for the files (default: the case folder; pasted/ for --text)")
	f.BoolVar(&reference, "reference", false, "put the files in references/: laws and rules")
	f.StringVar(&sourceURL, "source-url", "", "where you got the files, such as the page of a law")
	f.StringVar(&note, "note", "", "a note about where the files come from")
	return cmd
}

type statusResult struct{ *caseops.Status }

func (r statusResult) WriteText(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%d originals, %d derived files, %d notes.\n", r.Originals, r.Derived, r.Notes); err != nil {
		return err
	}
	for _, s := range r.Steps {
		mark := "done"
		if s.Pending > 0 {
			mark = fmt.Sprintf("%d", s.Pending)
		}
		if _, err := fmt.Fprintf(w, "  %-9s %-5s %s\n", s.Command, mark, s.Detail); err != nil {
			return err
		}
	}
	if r.Next == "" {
		_, err := fmt.Fprintln(w, "All done.")
		return err
	}
	_, err := fmt.Fprintf(w, "Next: openarch docs %s\n", r.Next)
	return err
}

func newStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is left to do in the case folder",
		Long: `Show, for each step, how many files are left: new files to ingest, PDFs and
images to extract, images to describe, texts to index, and files to sync to
OpenArch. It also counts files that are missing or changed (verify), and
names the next command. status needs no model and no OpenArch.`,
		Example: `  openarch docs status
  openarch docs status --output json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if err := requireManifest(root); err != nil {
				return err
			}
			st, err := caseops.GetStatus(cmd.Context(), root)
			if err != nil {
				return err
			}
			return app.Printer.Print(statusResult{st})
		},
	}
}

type collectResult struct{ *caseops.CollectResult }

func (r collectResult) WriteText(w io.Writer) error {
	for _, c := range r.Files {
		line := fmt.Sprintf("%s  <- %s", c.File, c.From)
		if c.Email != "" {
			line += fmt.Sprintf(" (email %q)", c.Email)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Collected %d files in %s, with collect.json.\n", len(r.Files), r.Out)
	return err
}

func newCollectCmd(app *App) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "collect <file>...",
		Short: "Copy files to attach into one folder, with their real names",
		Long: `Copy originals or files from extracted/ (such as an attachment of an email)
into one folder, ready to attach to an email. Attachments get the name they
had in the email. collect.json lists where each file came from.

The default folder is notes/outbox/<date>. Inside the case folder, collect
writes only in notes/outbox/, so the copies never become originals.`,
		Example: `  openarch docs collect justificante.pdf extracted/x-1a2b3c4d/attachments/plano.pdf
  openarch docs collect escritura.pdf --out ~/Desktop/para-enviar`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if err := requireManifest(root); err != nil {
				return err
			}
			refs := make([]string, len(args))
			for i, a := range args {
				refs[i] = a
				if abs, err := filepath.Abs(a); err == nil {
					if rel, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(rel, "..") {
						if _, err := os.Stat(abs); err == nil {
							refs[i] = filepath.ToSlash(rel)
						}
					}
				}
			}
			res, err := caseops.Collect(caseops.CollectOptions{Root: root, Refs: refs, Out: out})
			if err != nil {
				return output.Usage(err.Error(), "")
			}
			return app.Printer.Print(collectResult{res})
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "folder for the files (default: notes/outbox/<date>)")
	return cmd
}

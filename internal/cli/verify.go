package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/verify"
)

type verifyResult struct{ *verify.Result }

func (r verifyResult) WriteText(w io.Writer) error {
	for _, p := range r.Problems {
		line := fmt.Sprintf("%-8s %s", p.Kind, p.Path)
		if p.Detail != "" {
			line += ": " + p.Detail
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	for _, p := range r.New {
		if _, err := fmt.Fprintf(w, "%-8s %s\n", "new", p); err != nil {
			return err
		}
	}
	status := "OK"
	if !r.OK() {
		status = fmt.Sprintf("%d problems", len(r.Problems))
	}
	_, err := fmt.Fprintf(w, "%s: %d originals and %d derived files checked. %d new files not ingested.\n",
		status, r.Originals, r.Derived, len(r.New))
	return err
}

func newVerifyCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check the originals and derived files against the manifest",
		Long: `Check the originals and derived files against the SHA-256 checksums in
extracted/manifest.json, and read every file of each zip original.

Exit code 0 means every file matches. Exit code 1 means a file is missing or
changed, or a zip is broken. Files that are not ingested yet are listed as new;
they are not problems.`,
		Example: `  openarch docs verify
  openarch docs verify --output json`,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			res, err := verify.Run(root)
			if err != nil {
				return err
			}
			if err := app.Printer.Print(verifyResult{res}); err != nil {
				return err
			}
			if !res.OK() {
				return output.Exit(output.ExitError, "verify found problems")
			}
			return nil
		},
	}
}

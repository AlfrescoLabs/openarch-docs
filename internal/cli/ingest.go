package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
)

type ingestResult struct{ *ingest.Result }

func (r ingestResult) WriteText(w io.Writer) error {
	for _, f := range r.Files {
		if f.Status == ingest.StatusUnchanged {
			continue
		}
		if _, err := fmt.Fprintf(w, "%-9s %s (%s, %d derived)\n", f.Status, f.Path, f.Kind, f.Derived); err != nil {
			return err
		}
	}
	verb := "Ingested"
	if r.DryRun {
		verb = "Would ingest"
	}
	_, err := fmt.Fprintf(w, "%s %d new and %d changed files, %d unchanged. %d derived files. Manifest: %s\n",
		verb, r.Added, r.Changed, len(r.Files)-r.Added-r.Changed, r.Derived, r.Manifest)
	return err
}

func newIngestCmd(app *App) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Record the originals of the case folder and unpack emails and chat exports",
		Long: `Record the originals of the case folder and unpack emails and chat exports.

Every file in the case folder is an original, except extracted/, notes/, hidden
files and agent files (AGENTS.md, CLAUDE.md). Files in references/ are reference
texts, such as laws. Ingest never changes an original. It records its SHA-256 in
extracted/manifest.json and writes derived files to extracted/<name>-<hash>/:

- Emails (.eml): message.txt with the headers and the body text, message.html,
  attachments/, inline/ (inline images) and message-N/ for each attached email,
  with its own message.eml. The manifest keeps the sender, date and subject of
  the email that holds each file, the emails around it, and the forwarded
  messages quoted in the body.
- WhatsApp exports (.zip with _chat.txt): chat.txt, messages.json and media/.
  The manifest keeps who sent each media file and when.
- Other zips: their files in files/.

Run it again after you add files: unchanged files are skipped. A changed
original gets new derived files.`,
		Example: `  openarch docs ingest
  openarch docs ingest --case ~/cases/garage --dry-run
  openarch docs ingest --output json`,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			res, err := ingest.Run(ingest.Options{Root: root, DryRun: dryRun})
			if err != nil {
				return err
			}
			return app.Printer.Print(ingestResult{res})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would change and write nothing")
	return cmd
}

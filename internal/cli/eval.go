package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
	"github.com/AlfrescoLabs/openarch-docs/internal/eval"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
)

type evalResult struct{ *eval.Report }

func (r evalResult) WriteText(w io.Writer) error {
	for i, res := range r.Results {
		if _, err := fmt.Fprintf(w, "%2d %-7s %s\n", i+1, res.Status, res.Text); err != nil {
			return err
		}
		detail := ""
		switch {
		case res.Skip != "":
			detail = "skipped: " + res.Skip
		case res.Error != "":
			detail = "error: " + res.Error
		case len(res.Found) > 0:
			detail = "has: " + strings.Join(res.Found, ", ")
		case len(res.Missing) > 0:
			detail = "lacks: " + strings.Join(res.Missing, "; ")
		}
		if detail != "" {
			if _, err := fmt.Fprintf(w, "           %s\n", detail); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(w, "%d pass, %d partial, %d fail, %d skipped. Model: %s\n", r.Pass, r.Partial, r.Fail, r.Skip, r.Model)
	return err
}

func newEvalCmd(app *App) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "eval <questions-file>",
		Short: "Ask a set of questions and check the answers",
		Long: `Ask every question of a file with 'ask', and check each answer against the
words it must hold and must not hold. Use it to measure the plugin on the
questions of a real case, and to compare profiles.

The file has one block per question:

  # a comment
  Q: ¿Quién envió el plano?
  E: instalador | Rubén; correo     (each group, split by ;, needs one word)
  N: cliente                          (words that must not appear)
  S: needs the web                    (skip the question, with the reason)

Words are compared in lower case, without accents. An answer passes with
every group, is partial with at least half, and fails with less, or with a
forbidden word. Keep a question file of a private case out of any repository.`,
		Example: `  openarch docs eval ~/cases/garage-questions.txt
  openarch docs eval questions.txt --profile small --output json > report.json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if err := requireManifest(root); err != nil {
				return err
			}
			f, err := os.Open(args[0])
			if err != nil {
				return output.Usage(err.Error(), "")
			}
			defer func() { _ = f.Close() }()
			qs, err := eval.Parse(f)
			if err != nil {
				return output.Usage(args[0]+": "+err.Error(), "")
			}
			p, err := app.profile()
			if err != nil {
				return err
			}
			x, err := index.Open(cmd.Context(), root)
			if err != nil {
				return err
			}
			defer func() { _ = x.Close() }()
			client := app.modelClient()
			rep, err := eval.Run(cmd.Context(), qs, eval.Options{
				Ask:      ask.Options{Limit: limit, Index: x, Embedder: client, EmbedModel: p.Embed, Chat: client, ChatModel: p.Chat},
				Progress: app.Streams.Err,
			})
			if err != nil {
				return modelError(err, p.Chat)
			}
			return app.Printer.Print(evalResult{rep})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 12, "number of sources the model reads for each question")
	return cmd
}

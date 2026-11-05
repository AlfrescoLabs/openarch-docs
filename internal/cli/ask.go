package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
)

type askResult struct{ *ask.Result }

func (r askResult) WriteText(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s\n\nSources:\n", r.Answer); err != nil {
		return err
	}
	for _, s := range r.Sources {
		mark := " "
		for _, n := range r.Cited {
			if n == s.N {
				mark = "*"
			}
		}
		if _, err := fmt.Fprintf(w, "%s[%d] %s (%s)\n     %s\n", mark, s.N, s.Cite, s.Role, s.Path); err != nil {
			return err
		}
	}
	for _, c := range r.Checks {
		if _, err := fmt.Fprintf(w, "Check: sentence %d cites %v, but its words are in [%d].\n", c.Sentence, c.Cited, c.Better); err != nil {
			return err
		}
	}
	if r.Warning != "" {
		if _, err := fmt.Fprintf(w, "Warning: %s\n", r.Warning); err != nil {
			return err
		}
	}
	if len(r.Unknown) > 0 {
		if _, err := fmt.Fprintf(w, "Warning: the answer cites sources that do not exist: %v. Check it against the sources.\n", r.Unknown); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "* cited in the answer. Model: %s\n", r.Model)
	return err
}

func newAskCmd(app *App) *cobra.Command {
	var (
		limit     int
		withNotes bool
	)
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Answer a question about the case folder, with sources",
		Long: `Answer a question with the local chat model of the profile.

ask searches the index (case documents and references; notes too with
--with-notes), gives the model the best pieces as numbered sources, and asks
it to answer in the language of the question, citing a source for every fact
as [n]. The model makes no tool calls, so small models work.

The answer lists the sources, marks the cited ones, and warns when the model
cites a number that is not a source. Always check important facts in the
cited files.

Run index first.`,
		Example: `  openarch docs ask "¿Quién aportó las fotos de la solicitud?"
  openarch docs ask "What does the law say about charging points?" --limit 12
  openarch docs ask "Qual é o estado do pedido?" --with-notes --output json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if err := requireManifest(root); err != nil {
				return err
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
			res, err := ask.Run(cmd.Context(), ask.Options{
				Question: strings.Join(args, " "), Limit: limit, WithNotes: withNotes, Index: x,
				Embedder: client, EmbedModel: p.Embed, Chat: client, ChatModel: p.Chat,
			})
			if err != nil {
				model := p.Chat
				if strings.Contains(err.Error(), p.Embed) {
					model = p.Embed
				}
				return modelError(err, model)
			}
			return app.Printer.Print(askResult{res})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 8, "number of sources the model reads")
	cmd.Flags().BoolVar(&withNotes, "with-notes", false, "add notes to the sources, marked as interpretations")
	return cmd
}

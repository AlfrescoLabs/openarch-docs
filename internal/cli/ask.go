package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
)

type askResult struct{ *ask.Result }

func (r askResult) WriteText(w io.Writer) error {
	if r.Standalone != "" {
		if _, err := fmt.Fprintf(w, "Question: %s\n\n", r.Standalone); err != nil {
			return err
		}
	}
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
		limit                          int
		noNotes, expand, rerank, neigh bool
		think, cont, fresh             bool
	)
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Answer a question about the case folder, with sources",
		Long: `Answer a question with the local chat model of the profile.

ask searches the index with the question and adds up to 3 notes,
marked as interpretations (--no-notes leaves them out). It adds the newest
emails when the question asks for the last one, gives the model the best
pieces as numbered sources, and asks
it to answer in the language of the question, citing a source for every fact
as [n]. The model makes no tool calls, so small models work.

--expand also searches with 3 queries the model writes in other words,
--rerank lets the model pick the passages that help from 30 candidates, and
--neighbors adds the pieces around each one. They are off by default: on a
real case they added little and made answers about 3 times slower.

--continue makes the question part of a conversation: the model reads the
last 3 questions and answers, and first writes the question so that it
stands alone ("¿Y qué portal dijo esa persona?" becomes a question with the
name), which search uses. The conversation is kept in .ask/history.json in
the case folder. --new starts a new conversation. Without --continue or
--new, each question stands alone.

The answer lists the sources, marks the cited ones, and warns when the model
cites a number that is not a source. Always check important facts in the
cited files.

Run index first.`,
		Example: `  openarch docs ask "¿Quién aportó las fotos de la solicitud?"
  openarch docs ask "What does the law say about charging points?" --limit 12
  openarch docs ask "Qual é o estado do pedido?" --no-notes --output json
  openarch docs ask --new "¿Quién envió la foto del cuarto de contadores?"
  openarch docs ask --continue "¿Y qué portal dijo esa persona?"`,
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
			var history []ask.Turn
			if fresh {
				if err := ask.ClearHistory(root); err != nil {
					return err
				}
			} else if cont {
				if history, err = ask.LoadHistory(root); err != nil {
					return output.Usage(err.Error(), "Start a new conversation with --new.")
				}
			}
			client := app.modelClient()
			question := strings.Join(args, " ")
			res, err := ask.Run(cmd.Context(), ask.Options{
				Question: question, Limit: limit, NoNotes: noNotes, Think: think, Expand: expand, Rerank: rerank, Neighbors: neigh, Index: x,
				Embedder: client, EmbedModel: p.Embed, Chat: client, ChatModel: p.Chat, History: history,
			})
			if errors.Is(err, models.ErrNoAnswer) {
				return output.NewError(output.ExitRemote, err.Error(), "Ask a shorter question, or run without --think.")
			}
			if err != nil {
				model := p.Chat
				if strings.Contains(err.Error(), p.Embed) {
					model = p.Embed
				}
				return modelError(err, model)
			}
			if cont || fresh {
				if err := ask.SaveHistory(root, append(history, ask.Turn{Question: question, Answer: res.Answer})); err != nil {
					return err
				}
			}
			return app.Printer.Print(askResult{res})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 12, "number of sources the model reads")
	cmd.Flags().BoolVar(&noNotes, "no-notes", false, "leave the notes out of the sources")
	cmd.Flags().BoolVar(&expand, "expand", false, "also search with 3 queries the model writes in other words (slower)")
	cmd.Flags().BoolVar(&rerank, "rerank", false, "let the model pick the useful passages from 30 candidates (slower)")
	cmd.Flags().BoolVar(&neigh, "neighbors", false, "add the pieces around each hit")
	cmd.Flags().BoolVar(&cont, "continue", false, "continue the conversation: read the last questions and answers")
	cmd.Flags().BoolVar(&fresh, "new", false, "start a new conversation, kept for --continue")
	cmd.MarkFlagsMutuallyExclusive("continue", "new")
	cmd.Flags().BoolVar(&think, "think", false, "let models that think before they answer (GLM 4.7 Flash) do so: slower")
	return cmd
}

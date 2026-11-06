package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
	"github.com/AlfrescoLabs/openarch-docs/internal/eval"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
)

type evalResult struct{ *eval.Report }

func (r evalResult) WriteText(w io.Writer) error {
	for i, res := range r.Results {
		judge := " "
		if res.Judge >= 0 {
			judge = fmt.Sprint(res.Judge)
		}
		if _, err := fmt.Fprintf(w, "%2d %-7s %s %s\n", i+1, res.Status, judge, res.Text); err != nil {
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
	line := fmt.Sprintf("%d pass, %d partial, %d fail, %d skipped. Model: %s.", r.Pass, r.Partial, r.Fail, r.Skip, r.Model)
	if r.JudgeMax > 0 {
		line += fmt.Sprintf(" Judge: %d of %d (%d%%).", r.JudgeScore, r.JudgeMax, 100*r.JudgeScore/r.JudgeMax)
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func newEvalCmd(app *App) *cobra.Command {
	var limit int
	var noJudge, noExpand, noRerank, noNeigh, noNotes, judgeThink bool
	var rejudge string
	var judgeModel string
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
  A: El instalador, por correo.       (a correct answer, for the judge model)
  F: Lo envió el instalador.          (a key fact, one per line, for the judge)

Words are compared in lower case, without accents. An answer passes with
every group, is partial with at least half, and fails with less, or with a
forbidden word. With A: or F: lines, a judge model also scores each answer
(--judge-model, the chat model of the large profile by default): 2 right,
1 partly right, 0 wrong. With F: lines it checks each fact; with only an
A: line it lists the key facts of the reference itself, which is less steady. Keep a question file of a private case out of any repository.`,
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
			client := app.modelClient()
			jopts := eval.Options{Judge: judge(client, noJudge), JudgeModel: judgeModel, JudgeThink: judgeThink}
			if rejudge != "" {
				data, err := os.ReadFile(rejudge)
				if err != nil {
					return output.Usage(err.Error(), "")
				}
				var old eval.Report
				if err := json.Unmarshal(data, &old); err != nil {
					return output.Usage(rejudge+": "+err.Error(), "Give a report written by eval --output json.")
				}
				rep, err := eval.Rejudge(cmd.Context(), &old, qs, jopts)
				if err != nil {
					return modelError(err, judgeModel)
				}
				return app.Printer.Print(evalResult{rep})
			}
			x, err := index.Open(cmd.Context(), root)
			if err != nil {
				return err
			}
			defer func() { _ = x.Close() }()
			rep, err := eval.Run(cmd.Context(), qs, eval.Options{
				Ask: ask.Options{Limit: limit, Index: x, Embedder: client, EmbedModel: p.Embed, Chat: client, ChatModel: p.Chat,
					NoExpand: noExpand, NoRerank: noRerank, NoNeighbors: noNeigh, NoNotes: noNotes},
				Progress: app.Streams.Err,
				Judge:    jopts.Judge, JudgeModel: judgeModel, JudgeThink: judgeThink,
			})
			if err != nil {
				return modelError(err, p.Chat)
			}
			return app.Printer.Print(evalResult{rep})
		},
	}
	cmd.Flags().BoolVar(&noJudge, "no-judge", false, "do not judge the answers")
	cmd.Flags().BoolVar(&judgeThink, "judge-think", false, "let the judge think before it scores: slower, steadier")
	cmd.Flags().StringVar(&rejudge, "rejudge", "", "score the answers of this JSON report again, without asking again")
	cmd.Flags().BoolVar(&noExpand, "no-expand", false, "ask without query expansion (to measure it)")
	cmd.Flags().BoolVar(&noRerank, "no-rerank", false, "ask without rerank (to measure it)")
	cmd.Flags().BoolVar(&noNeigh, "no-neighbors", false, "ask without neighbor pieces (to measure them)")
	cmd.Flags().BoolVar(&noNotes, "no-notes", false, "ask without notes (to measure them)")
	large, _ := models.GetProfile("large")
	cmd.Flags().StringVar(&judgeModel, "judge-model", large.Chat, "model that judges the answers; the same for every profile, so runs compare")
	cmd.Flags().IntVar(&limit, "limit", 12, "number of sources the model reads for each question")
	return cmd
}

// judge returns the judge, or nil when it is turned off.
func judge(c ask.Chatter, off bool) ask.Chatter {
	if off {
		return nil
	}
	return c
}

package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/vision"
)

type describeResult struct{ *vision.Result }

func (r describeResult) WriteText(w io.Writer) error {
	for _, it := range r.Items {
		if it.Status == vision.StatusSkipped {
			continue
		}
		label := it.Path
		if it.Page > 0 {
			label = fmt.Sprintf("%s p.%d", it.From, it.Page)
		}
		line := fmt.Sprintf("%-11s %s", it.Status, label)
		if it.Error != "" {
			line += ": " + it.Error
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	line := fmt.Sprintf("Described %d images, %d already done, %d small (logos), %d unsupported, %d failed. Model: %s, language: %s.",
		r.Done, r.Skipped, r.Small, r.Unsupported, r.Failed, r.Model, r.Language)
	if r.Removed > 0 {
		line += fmt.Sprintf(" Removed %d descriptions of text pages.", r.Removed)
	}
	if r.Pending > 0 {
		line += fmt.Sprintf(" %d images left; run describe again.", r.Pending)
	}
	_, err := fmt.Fprintln(w, line+" Run index to search the descriptions.")
	return err
}

func newDescribeCmd(app *App) *cobra.Command {
	var (
		langCode string
		allPages bool
		force    bool
		maxItems int
	)
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "Describe photos, plans and image pages with the vision model",
		Long: `Write a text description of each photo, plan, screenshot and PDF page with
large images, with the local vision model of the profile. Pages with a lot of
text (600 letters or more in the text layer or OCR), such as a scanned deed,
are not described: their text says it, and descriptions of them crowd the
search results. Descriptions of such pages from earlier runs are removed. Run extract first,
and index after, so search and ask find the descriptions.

Each description lists the type of image, every readable text, what the image
shows, and any address, floor, room or number that tells where it is. It goes
next to the image (describe.txt, or page-NNN.describe.txt for a PDF page), and
the manifest records the model. Images smaller than about 200 x 200 pixels,
such as logos, are skipped.

The descriptions are written in the main language of the case, found from its
texts, or in --lang. With the large profile, the chat models of the profile
are unloaded first, so the vision model has memory.

The vision model can be wrong. Treat a description as a pointer to the image,
and check important facts in the image itself.`,
		Example: `  openarch docs describe
  openarch docs describe --max 10
  openarch docs describe --lang es --all-pages --force`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			language, err := app.describeLanguage(root, langCode)
			if err != nil {
				return err
			}
			if p.Name == "large" {
				models.Unload(cmd.Context(), p.Agent, p.Chat)
			}
			res, err := vision.Describe(cmd.Context(), vision.Options{
				Root: root, Chat: app.modelClient(), Model: p.Vision, Language: language,
				AllPages: allPages, Force: force, Max: maxItems, Progress: app.Streams.Err,
			})
			if err != nil {
				return modelError(err, p.Vision)
			}
			if err := app.Printer.Print(describeResult{res}); err != nil {
				return err
			}
			if res.Failed > 0 {
				return output.Exit(output.ExitError, "describe failed for some images")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&langCode, "lang", "", "language of the descriptions, as an ISO 639-1 code (default: the main language of the case)")
	f.BoolVar(&allPages, "all-pages", false, "also describe PDF pages without large images")
	f.BoolVar(&force, "force", false, "describe again images that have a description")
	f.IntVar(&maxItems, "max", 0, "stop after this many descriptions (0: no limit)")
	_ = cmd.RegisterFlagCompletionFunc("lang", cobra.FixedCompletions(lang.Codes(), cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// describeLanguage returns --lang, else the main language of the case, else English.
func (a *App) describeLanguage(root, code string) (lang.Language, error) {
	if code != "" {
		ls, err := lang.Get([]string{code})
		if err != nil {
			return lang.Language{}, output.Usage(err.Error(), "")
		}
		return ls[0], nil
	}
	if l, ok := vision.CaseLanguage(root); ok {
		return l, nil
	}
	ls, _ := lang.Get([]string{"en"})
	return ls[0], nil
}

type lookResult struct{ *vision.LookResult }

func (r lookResult) WriteText(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "%s\n\nImages:\n", r.Answer); err != nil {
		return err
	}
	for _, im := range r.Images {
		if _, err := fmt.Fprintf(w, " Image %d: %s\n          %s\n", im.N, im.Label, im.Path); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Model: %s. The vision model can be wrong; check the images.\n", r.Model)
	return err
}

func newLookCmd(app *App) *cobra.Command {
	var question string
	cmd := &cobra.Command{
		Use:   "look <image>... --question <text>",
		Short: "Ask the vision model one question about one or more images",
		Long: `Send one or more images and one question to the local vision model of the
profile, for example to compare photos with a plan.

An image is a path in the case folder (an original, or a file in extracted/),
or <pdf>#<page> for a page of a PDF that extract rendered. The model refers to
the images as Image 1, Image 2, in the order given. It answers in the language
of the question. Nothing is saved.`,
		Example: `  openarch docs look extracted/x/media/photo.jpg "condiciones.pdf#4" -q "¿Es el mismo cuarto de contadores?"
  openarch docs look catastro-207.png -q "What floor and door does this show?"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if question == "" {
				return output.Usage("--question is required", "Add -q \"your question\".")
			}
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
			if p.Name == "large" {
				models.Unload(cmd.Context(), p.Agent, p.Chat)
			}
			res, err := vision.Look(cmd.Context(), root, app.modelClient(), p.Vision, question, args)
			if err != nil {
				var me *models.ModelError
				if errors.Is(err, models.ErrUnreachable) || errors.As(err, &me) {
					return modelError(err, p.Vision)
				}
				return output.Usage(err.Error(), "")
			}
			return app.Printer.Print(lookResult{res})
		},
	}
	cmd.Flags().StringVarP(&question, "question", "q", "", "the question about the images")
	return cmd
}

package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/extract"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/tools"
)

const envTools = "OPENARCH_DOCS_TOOLS"

type extractResult struct{ *extract.Result }

func (r extractResult) WriteText(w io.Writer) error {
	for _, f := range r.Files {
		if f.Status == extract.StatusSkipped {
			continue
		}
		line := fmt.Sprintf("%-11s %s", f.Status, f.Path)
		switch {
		case f.Error != "":
			line += ": " + f.Error
		case f.Pages > 0:
			line += fmt.Sprintf(" (%d pages, OCR on %s, images on %s)", f.Pages, pageList(f.OCRPages), pageList(f.ImagePages))
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "Extracted %d files, %d already done, %d unsupported, %d failed. OCR languages: %s\n",
		r.Done, r.Skipped, r.Unsupported, r.Failed, strings.Join(r.Languages, ", "))
	return err
}

func pageList(pages []int) string {
	if len(pages) == 0 {
		return "none"
	}
	s := make([]string, len(pages))
	for i, p := range pages {
		s[i] = fmt.Sprint(p)
	}
	return "p. " + strings.Join(s, ", ")
}

func newExtractCmd(app *App) *cobra.Command {
	var (
		toolsMode string
		langs     []string
		dpi       int
		minText   int
		force     bool
	)
	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Get the text of PDFs and images, with OCR for scans",
		Long: `Get the text of every PDF and image of the case folder: originals, and files
that ingest unpacked from emails and zips. Run ingest first.

For each PDF page it writes page-NNN.txt (the text layer), page-NNN.png (an
image of the page) and, when the page has little text, page-NNN.ocr.txt (OCR).
The manifest marks the pages with large images, such as photos and plans, so
they are not missed. Images get ocr.txt.

OCR reads English, Spanish, French and Portuguese by default. Fewer languages
with --lang make OCR faster.

The programs (poppler and tesseract) run in the openarch-docs tools image with
no network. The first run builds the image, which downloads packages once.
With --tools local they run from PATH instead.

Files that already have results are skipped; --force extracts them again.`,
		Example: `  openarch docs extract
  openarch docs extract --lang es,en
  openarch docs extract --tools local --dpi 200
  openarch docs extract --force --output json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			languages, err := lang.Get(langs)
			if err != nil {
				return output.Usage(err.Error(), "")
			}
			var runner tools.Runner
			switch toolsMode {
			case "docker":
				d := tools.Docker{Log: app.Streams.Err}
				if err := d.EnsureImage(cmd.Context()); err != nil {
					if errors.Is(err, tools.ErrNoDocker) {
						return output.NewError(output.ExitPrerequisite, "docker is not on PATH", "Install Docker, or use --tools local with poppler and tesseract installed.")
					}
					return output.NewError(output.ExitPrerequisite, err.Error(), "Check that Docker is running and can download packages once.")
				}
				runner = d
			case "local":
				l := tools.Local{}
				if missing := l.Missing(); len(missing) > 0 {
					return output.NewError(output.ExitPrerequisite, "missing programs: "+strings.Join(missing, ", "), "Install poppler and tesseract, or use --tools docker.")
				}
				runner = l
			default:
				return output.Usage(fmt.Sprintf("unknown --tools %q", toolsMode), "Use docker or local.")
			}
			res, err := extract.Run(cmd.Context(), extract.Options{
				Root: root, Runner: runner, Languages: languages, DPI: dpi, MinText: minText, Force: force, Progress: app.Streams.Err,
			})
			if err != nil {
				return err
			}
			if err := app.Printer.Print(extractResult{res}); err != nil {
				return err
			}
			if res.Failed > 0 {
				return output.Exit(output.ExitError, "extract failed for some files")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&toolsMode, "tools", envOr(envTools, "docker"), "where the programs run: docker or local (env "+envTools+")")
	f.StringSliceVar(&langs, "lang", lang.Codes(), "OCR languages, as ISO 639-1 codes")
	f.IntVar(&dpi, "dpi", 150, "resolution of the page images")
	f.IntVar(&minText, "min-text", 50, "pages with fewer letters and digits than this get OCR")
	f.BoolVar(&force, "force", false, "extract again files that already have results")
	_ = cmd.RegisterFlagCompletionFunc("tools", cobra.FixedCompletions([]string{"docker", "local"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("lang", cobra.FixedCompletions(lang.Codes(), cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

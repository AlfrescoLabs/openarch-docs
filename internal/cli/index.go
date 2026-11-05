package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
)

type indexResult struct{ *index.BuildResult }

func (r indexResult) WriteText(w io.Writer) error {
	line := fmt.Sprintf("Indexed %d texts: %d updated, %d removed. %d chunks, %d embedded now", r.Files, r.Updated, r.Removed, r.Chunks, r.Embedded)
	if r.Model != "" {
		line += " with " + r.Model
	}
	line += "."
	if r.Pending > 0 {
		line += fmt.Sprintf(" %d chunks have no embeddings yet; run index again without --no-embed.", r.Pending)
	}
	_, err := fmt.Fprintf(w, "%s Index: %s\n", line, r.IndexFile)
	return err
}

func newIndexCmd(app *App) *cobra.Command {
	var noEmbed bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Update the search index of the case folder",
		Long: `Update the search index in extracted/index.sqlite from the texts that ingest
and extract wrote, the text originals, and the notes in notes/.

Each text is split into chunks of about 800 characters. Each chunk gets full
text search (accents folded, so "sotano" finds "sótano") and an embedding from
the embedding model of the profile, on Docker Model Runner. Only new and
changed texts are processed. With --no-embed only full text search is updated.

The index is a cache. Deleting it only costs a rebuild.`,
		Example: `  openarch docs index
  openarch docs index --no-embed
  openarch docs index --profile large --output json`,
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
			x, err := index.Open(cmd.Context(), root)
			if err != nil {
				return err
			}
			defer func() { _ = x.Close() }()
			opts := index.BuildOptions{Root: root, Progress: app.Streams.Err}
			if !noEmbed {
				opts.Embedder, opts.Model = app.modelClient(), p.Embed
			}
			res, err := x.Build(cmd.Context(), opts)
			if err != nil {
				return modelError(err, p.Embed)
			}
			return app.Printer.Print(indexResult{res})
		},
	}
	cmd.Flags().BoolVar(&noEmbed, "no-embed", false, "update full text search only, without the embedding model")
	return cmd
}

type searchResult struct {
	Query string      `json:"query"`
	Hits  []index.Hit `json:"hits"`
}

func (r searchResult) WriteText(w io.Writer) error {
	if len(r.Hits) == 0 {
		_, err := fmt.Fprintln(w, "No results.")
		return err
	}
	for _, h := range r.Hits {
		text := strings.Join(strings.Fields(h.Text), " ")
		if len([]rune(text)) > 240 {
			text = string([]rune(text)[:240]) + "..."
		}
		if _, err := fmt.Fprintf(w, "%d. [%s] (%s, %s)\n   %s\n   %s\n", h.Rank, h.Cite, h.Role, strings.Join(h.Matched, "+"), h.Path, text); err != nil {
			return err
		}
	}
	return nil
}

func newSearchCmd(app *App) *cobra.Command {
	var (
		limit   int
		roles   []string
		noEmbed bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the case folder texts",
		Long: `Search the index by words (full text) and by meaning (embeddings), and merge
both lists. Each result cites its file and page, and says whether it is a case
document, a reference text (references/) or a note (notes/).

Run index first. With --no-embed only full text search runs.`,
		Example: `  openarch docs search "cuarto de contadores"
  openarch docs search who sent the photos --limit 5
  openarch docs search ley propiedad horizontal --role reference --output json`,
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
			opts := index.SearchOptions{Query: strings.Join(args, " "), Limit: limit, Roles: roles}
			if !noEmbed {
				opts.Embedder, opts.Model = app.modelClient(), p.Embed
			}
			hits, err := x.Search(cmd.Context(), opts)
			if err != nil {
				return modelError(err, p.Embed)
			}
			if hits == nil {
				hits = []index.Hit{}
			}
			return app.Printer.Print(searchResult{Query: opts.Query, Hits: hits})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "number of results")
	cmd.Flags().StringSliceVar(&roles, "role", nil, "keep only these roles: "+strings.Join([]string{casefile.RoleCase, casefile.RoleReference, index.RoleNote}, ", "))
	cmd.Flags().BoolVar(&noEmbed, "no-embed", false, "search by words only, without the embedding model")
	_ = cmd.RegisterFlagCompletionFunc("role", cobra.FixedCompletions([]string{casefile.RoleCase, casefile.RoleReference, index.RoleNote}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

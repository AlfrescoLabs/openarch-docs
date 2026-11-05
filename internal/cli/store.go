package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/openarch"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/store"
)

// openarchClient returns the openarch CLI client.
func openarchClient(contextName string) (*openarch.CLI, error) {
	c, err := openarch.NewCLI(contextName)
	if errors.Is(err, openarch.ErrNoCLI) {
		return nil, output.NewError(output.ExitPrerequisite, err.Error(), "Install openarch: https://github.com/AlfrescoLabs/openarch-cli")
	}
	return c, err
}

// openarchError keeps the exit code of an openarch error, so a failed login
// stays exit code 6 and a missing OpenArch stays exit code 4 or 5.
func openarchError(err error) error {
	var e *openarch.Error
	if errors.As(err, &e) {
		hint := e.Hint
		if strings.Contains(e.Message, "unknown flag") {
			hint = "This openarch is too old for the plugin (it needs documents upload --replace and --description). Run 'openarch self-update'."
		}
		return output.NewError(output.ExitCode(e.Exit), "openarch: "+e.Message, hint)
	}
	return err
}

type syncResult struct{ *store.SyncResult }

func (r syncResult) WriteText(w io.Writer) error {
	verb := "Uploaded"
	if r.DryRun {
		verb = "Would upload"
	}
	_, err := fmt.Fprintf(w, "%s %d files to %s, %d unchanged, %d folders created.\n", verb, len(r.Uploaded), r.Remote, r.Unchanged, r.Folders)
	return err
}

func newSyncCmd(app *App) *cobra.Command {
	var (
		remote, contextName string
		originalsOnly       bool
		dryRun              bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Upload the case folder to OpenArch",
		Long: `Upload the originals and the derived files of the case folder to OpenArch,
the store of record. Originals go to <remote>/originals/ and derived files to
<remote>/extracted/, with the same folders as on disk. The default remote is
/cases/<name of the case folder>.

The sys_description of each document holds its manifest entry: where the file
comes from, the email or chat message that sent it, the page, the method, the
model and the SHA-256. So OpenArch holds what is needed to rebuild the case
folder ('reindex').

extracted/sync.json records what is uploaded; only new and changed files are
sent. A changed file replaces the content of its document. The search index
is not uploaded: it is a cache.

The plugin runs openarch for every call, so the workspace, the context and the
credentials are the ones of openarch. --openarch-context picks a context.`,
		Example: `  openarch docs sync
  openarch docs sync --dry-run
  openarch docs sync --remote /cases/garage-2026 --openarch-context prod`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if err := requireManifest(root); err != nil {
				return err
			}
			if remote == "" {
				remote = store.DefaultRemote(root)
			}
			if err := checkRemote(remote); err != nil {
				return err
			}
			c, err := openarchClient(contextName)
			if err != nil {
				return err
			}
			res, err := store.Sync(cmd.Context(), store.SyncOptions{
				Root: root, Remote: remote, Client: c, OriginalsOnly: originalsOnly, DryRun: dryRun, Progress: app.Streams.Err,
			})
			if err != nil {
				return openarchError(err)
			}
			return app.Printer.Print(syncResult{res})
		},
	}
	f := cmd.Flags()
	f.StringVar(&remote, "remote", "", "OpenArch folder of the case (default: /cases/<name of the case folder>)")
	f.StringVar(&contextName, "openarch-context", "", "openarch context to use (default: the one of openarch)")
	f.BoolVar(&originalsOnly, "originals-only", false, "upload only the originals")
	f.BoolVar(&dryRun, "dry-run", false, "list what would be uploaded and upload nothing")
	return cmd
}

// checkRemote rejects a remote folder that is not an absolute OpenArch path.
func checkRemote(remote string) error {
	if !strings.HasPrefix(remote, "/") || remote == "/" || strings.Contains(remote, "//") || strings.HasSuffix(remote, "/") {
		return output.Usage(fmt.Sprintf("bad --remote %q", remote), "Use a folder path such as /cases/garage.")
	}
	return nil
}

type reindexResult struct {
	*store.RestoreResult
	Index *index.BuildResult `json:"index,omitempty"`
}

func (r reindexResult) WriteText(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "Restored from %s: %d downloaded, %d already here, %d other documents skipped.\n",
		r.Remote, r.Downloaded, r.Kept, r.Skipped); err != nil {
		return err
	}
	for _, c := range r.Conflicts {
		if _, err := fmt.Fprintf(w, "conflict  %s: the local original differs from OpenArch and was kept\n", c); err != nil {
			return err
		}
	}
	if r.Index != nil {
		return indexResult{r.Index}.WriteText(w)
	}
	return nil
}

func newReindexCmd(app *App) *cobra.Command {
	var (
		remote, contextName string
		noEmbed, noIndex    bool
	)
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the case folder and its search index from OpenArch",
		Long: `Download the files that 'sync' uploaded to OpenArch, rebuild the manifest from
their descriptions, and update the search index. Use it on a new computer, or
after extracted/ was deleted.

Files that already match OpenArch are kept. A local original that differs from
OpenArch is never overwritten: it is listed as a conflict. Documents in the
folder that the plugin did not upload are skipped. Local files that were never
synced keep their manifest entries.`,
		Example: `  mkdir garage && cd garage && openarch docs reindex --remote /cases/garage
  openarch docs reindex --no-embed`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if remote == "" {
				remote = store.DefaultRemote(root)
			}
			if err := checkRemote(remote); err != nil {
				return err
			}
			c, err := openarchClient(contextName)
			if err != nil {
				return err
			}
			res, err := store.Restore(cmd.Context(), store.RestoreOptions{Root: root, Remote: remote, Client: c, Progress: app.Streams.Err})
			if err != nil {
				return openarchError(err)
			}
			out := reindexResult{RestoreResult: res}
			if !noIndex {
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
				if out.Index, err = x.Build(cmd.Context(), opts); err != nil {
					return modelError(err, p.Embed)
				}
			}
			return app.Printer.Print(out)
		},
	}
	f := cmd.Flags()
	f.StringVar(&remote, "remote", "", "OpenArch folder of the case (default: /cases/<name of the case folder>)")
	f.StringVar(&contextName, "openarch-context", "", "openarch context to use (default: the one of openarch)")
	f.BoolVar(&noEmbed, "no-embed", false, "update full text search only, without the embedding model")
	f.BoolVar(&noIndex, "no-index", false, "restore the files only, without updating the index")
	return cmd
}

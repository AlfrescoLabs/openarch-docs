package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/timeline"
)

type timelineResult struct {
	Events []timeline.Event `json:"events"`
}

func (r timelineResult) WriteText(w io.Writer) error {
	if len(r.Events) == 0 {
		_, err := fmt.Fprintln(w, "No events.")
		return err
	}
	for _, e := range r.Events {
		when := e.Time
		if when == "" {
			when = "? " + e.Raw
		}
		what := e.Subject
		if what == "" {
			what = "(no subject)"
		}
		if e.Kind == timeline.KindChat {
			what = strings.Join(strings.Fields(e.Text), " ")
		}
		if _, err := fmt.Fprintf(w, "%-16s  %-9s  %s: %s\n                   %s\n", when, e.Kind, e.From, what, e.Path); err != nil {
			return err
		}
	}
	return nil
}

func newTimelineCmd(app *App) *cobra.Command {
	var (
		since, until string
		kinds        []string
		undated      bool
	)
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "List the dated events of the case folder in time order",
		Long: `List the emails, the forwarded messages quoted in their bodies, and the chat
messages of the case folder, oldest first, with the file each one is in.

Dates of quoted messages are read in English, Spanish, French and Portuguese.
A message that is both an email and a quoted forward is listed once. Times are
in the time zone the source used. Events whose date cannot be read are left
out; --undated lists them at the end.

timeline needs no model. Run ingest first.`,
		Example: `  openarch docs timeline
  openarch docs timeline --since 2026-10-01 --kind email,forwarded
  openarch docs timeline --undated --output json`,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			if err := requireManifest(root); err != nil {
				return err
			}
			for _, d := range []string{since, until} {
				if d != "" && timeline.ParseDate(d) == "" {
					return output.Usage(fmt.Sprintf("bad date %q", d), "Use YYYY-MM-DD.")
				}
			}
			events, err := timeline.Build(timeline.Options{Root: root, Since: since, Until: until, Kinds: kinds, Undated: undated})
			if err != nil {
				return err
			}
			if events == nil {
				events = []timeline.Event{}
			}
			return app.Printer.Print(timelineResult{Events: events})
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "first day, YYYY-MM-DD")
	f.StringVar(&until, "until", "", "last day, YYYY-MM-DD")
	f.StringSliceVar(&kinds, "kind", nil, "keep only these kinds: email, forwarded, chat")
	f.BoolVar(&undated, "undated", false, "also list events whose date cannot be read")
	_ = cmd.RegisterFlagCompletionFunc("kind", cobra.FixedCompletions([]string{timeline.KindEmail, timeline.KindForwarded, timeline.KindChat}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

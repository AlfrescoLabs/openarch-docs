package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/AlfrescoLabs/openarch-docs/internal/mcptools"
	"github.com/AlfrescoLabs/openarch-docs/internal/notes"
	"github.com/AlfrescoLabs/openarch-docs/internal/output"
	"github.com/AlfrescoLabs/openarch-docs/internal/version"
	"github.com/AlfrescoLabs/openarch-docs/skills"
)

func newMCPCmd(app *App) *cobra.Command {
	var readOnly, noVision, noEmbed bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP server on stdio with tools for the case folder",
		Long: `Run an MCP server on stdio, for agents such as Docker Agent, with these tools:

  search      search texts by words and meaning, with citations
  read        read a text file of the case, with its provenance
  files       list files with their provenance (attachments, photos, pages)
  timeline    emails, quoted forwards and chat messages in time order
  look        ask the vision model about images (not with --no-vision)
  verify      check the checksums
  note_write  write a note in notes/ (not with --read-only)

Every tool reads, except note_write, which writes only in notes/. The tools
use the models of --profile. Prepare the case folder first: ingest, extract,
describe and index.`,
		Example: `  openarch docs mcp
  openarch docs mcp --case ~/cases/garage --profile large
  openarch docs mcp --read-only --no-vision`,
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
			client := app.modelClient()
			opts := mcptools.Options{Root: root, Version: version.Get().Version, ReadOnly: readOnly}
			if !noEmbed {
				opts.Embedder, opts.EmbedModel = client, p.Embed
			}
			if !noVision {
				opts.Vision, opts.VisionModel = client, p.Vision
			}
			return mcptools.Run(cmd.Context(), opts)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&readOnly, "read-only", false, "leave out note_write")
	f.BoolVar(&noVision, "no-vision", false, "leave out look")
	f.BoolVar(&noEmbed, "no-embed", false, "search by words only, without the embedding model")
	return cmd
}

type noteResult struct {
	Path string `json:"path"`
}

func (r noteResult) WriteText(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Wrote %s. Run index to make it searchable.\n", r.Path)
	return err
}

func newNoteCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Write notes and drafts in notes/",
		Args:  noArgs,
	}
	var overwrite bool
	var file string
	write := &cobra.Command{
		Use:   "write <name>",
		Short: "Write a note in notes/ from stdin or a file",
		Long: `Write a Markdown note or draft in the notes/ folder of the case. The name has
letters, digits, '.', '-' and '_', and at most one folder, such as
drafts/reply.md. A name without .md or .txt gets .md.

Notes are indexed with the role "note": search and ask treat them as
interpretations, not as evidence.`,
		Example: `  openarch docs note write status-2026-11-05 < status.md
  openarch docs note write drafts/reply.md --file reply.md --overwrite`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			root, err := app.caseRoot()
			if err != nil {
				return err
			}
			var data []byte
			if file != "" {
				data, err = os.ReadFile(file)
			} else {
				data, err = io.ReadAll(io.LimitReader(app.Streams.In, notes.MaxSize+1))
			}
			if err != nil {
				return output.Usage("cannot read the note: "+err.Error(), "")
			}
			p, err := notes.Write(root, args[0], string(data), overwrite)
			if errors.Is(err, notes.ErrExists) {
				return output.Usage(err.Error(), "Add --overwrite to replace it.")
			}
			if err != nil {
				return output.Usage(err.Error(), "")
			}
			return app.Printer.Print(noteResult{Path: p})
		},
	}
	write.Flags().BoolVar(&overwrite, "overwrite", false, "replace a note with the same name")
	write.Flags().StringVar(&file, "file", "", "read the note from this file instead of stdin")
	cmd.AddCommand(write)
	return cmd
}

type skillResult struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

func (r skillResult) WriteText(w io.Writer) error {
	if r.Action == skills.Unchanged {
		_, err := fmt.Fprintf(w, "The skill is up to date: %s\n", r.Path)
		return err
	}
	_, err := fmt.Fprintf(w, "Installed the skill: %s\n", r.Path)
	return err
}

func newSkillCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the Agent Skill that teaches agents to work with a case folder",
		Args:  noArgs,
	}
	var dir string
	var force bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Write the skill to .claude/skills/openarch-docs/SKILL.md",
		Long: `Write the skill to .claude/skills/openarch-docs/SKILL.md under --dir, or the
case folder. Agents that read Agent Skills (Docker Agent with skills: true,
Claude Code) load it when a task is about the case. A SKILL.md that differs
from this release is replaced only with --force.`,
		Example: `  openarch docs skill install
  openarch docs skill install --dir ~`,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error {
			target := dir
			if target == "" {
				root, err := app.caseRoot()
				if err != nil {
					return err
				}
				target = root
			}
			p, action, err := skills.Install(target, force)
			if errors.Is(err, skills.ErrChanged) {
				return output.Usage(p+": "+err.Error(), "Add --force to replace it.")
			}
			if err != nil {
				return err
			}
			return app.Printer.Print(skillResult{Path: p, Action: action})
		},
	}
	install.Flags().StringVar(&dir, "dir", "", "folder that gets .claude/skills (default: the case folder)")
	install.Flags().BoolVar(&force, "force", false, "replace a SKILL.md that differs")
	show := &cobra.Command{
		Use:   "show",
		Short: "Print the skill",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			_, err := app.Streams.Out.Write(skills.SKILL)
			return err
		},
	}
	cmd.AddCommand(install, show)
	return cmd
}

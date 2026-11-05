// Package mcptools is the MCP server of openarch-docs: tools that let an
// agent search and read a case folder, look at its images, and write notes.
// Every tool reads, except note_write, which writes only in notes/.
package mcptools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/notes"
	"github.com/AlfrescoLabs/openarch-docs/internal/timeline"
	"github.com/AlfrescoLabs/openarch-docs/internal/verify"
	"github.com/AlfrescoLabs/openarch-docs/internal/vision"
)

// maxResult cuts longer results, so one result cannot fill the context of a
// small model.
const maxResult = 16000

// Options configure the server.
type Options struct {
	Root    string
	Version string
	// Embedder and EmbedModel add meaning to search. Nil means words only.
	Embedder   index.Embedder
	EmbedModel string
	// Vision and VisionModel answer look. Nil removes the tool.
	Vision      vision.Chatter
	VisionModel string
	// ReadOnly removes note_write.
	ReadOnly bool
}

// New returns the MCP server.
func New(opts Options) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "openarch-docs", Version: opts.Version}, nil)
	t := &tools{opts: opts}
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(s, &mcp.Tool{Name: "search", Annotations: ro, Description: "Search the texts of the case folder by words and by meaning: emails, chat messages, PDF pages, OCR, image descriptions, references (laws) and notes. " +
		"Each hit has a citation (cite), the file path to read it with the read tool, its role (case, reference, note), sender and date. Notes are interpretations, not evidence."}, t.search)
	mcp.AddTool(s, &mcp.Tool{Name: "read", Annotations: ro, Description: "Read a text file of the case folder by its path, as given by search or files: email bodies (message.txt), page texts, OCR, image descriptions, chat messages, notes, references. " +
		"The result has the provenance of the file: the email or chat message that sent it, the page, the method. Long files come in parts: call again with the offset in next_offset."}, t.read)
	mcp.AddTool(s, &mcp.Tool{Name: "files", Annotations: ro, Description: "List the files of the case folder with their provenance: originals, email bodies, attachments, inline images, chat photos, page images and texts. " +
		"Filter by kind (for example attachment, media, page-image, description) or by a path prefix. Use it to find photos, plans and attachments, and who sent them."}, t.files)
	mcp.AddTool(s, &mcp.Tool{Name: "timeline", Annotations: ro, Description: "List the emails, the forwarded messages quoted in emails and the chat messages, oldest first, with sender, subject and file path. Dates are YYYY-MM-DD."}, t.timeline)
	mcp.AddTool(s, &mcp.Tool{Name: "verify", Annotations: ro, Description: "Check that the originals and derived files of the case folder still match their SHA-256 checksums."}, t.verify)
	if opts.Vision != nil {
		mcp.AddTool(s, &mcp.Tool{Name: "look", Annotations: ro, Description: "Ask the local vision model one question about one or more images of the case folder: photos, plans, screenshots, or PDF pages given as <pdf path>#<page>. " +
			"Use it to see what an image shows or to compare images; you cannot see images yourself. The vision model can be wrong."}, t.look)
	}
	if !opts.ReadOnly {
		mcp.AddTool(s, &mcp.Tool{Name: "note_write", Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(false)},
			Description: "Write a Markdown note or draft in the notes/ folder of the case, such as a status note, a timeline or an email draft (drafts/reply.md). " +
				"This is the only tool that writes. It never changes case documents. A name that exists needs overwrite: true."}, t.noteWrite)
	}
	return s
}

// Run serves on stdin and stdout.
func Run(ctx context.Context, opts Options) error {
	return New(opts).Run(ctx, &mcp.StdioTransport{})
}

func boolPtr(b bool) *bool { return &b }

type tools struct{ opts Options }

// result returns v as JSON text, cut to maxResult.
func result(v any) (*mcp.CallToolResult, any, error) {
	data, err := casefile.MarshalJSON(v)
	if err != nil {
		return nil, nil, err
	}
	text := string(data)
	if len(text) > maxResult {
		cut := text[:maxResult]
		for !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		text = cut + "\n... (result cut; ask for less, such as a smaller limit)"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func toolError(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
}

// SearchInput is the input of search.
type SearchInput struct {
	Query string   `json:"query" jsonschema:"the words or the question to search for"`
	Limit int      `json:"limit,omitempty" jsonschema:"number of hits, 1 to 20; default 8"`
	Roles []string `json:"roles,omitempty" jsonschema:"keep only these roles: case, reference, note"`
}

// maxHitText cuts the text of each hit; read gives the whole file.
const maxHitText = 1200

func (t *tools) search(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Query) == "" {
		return toolError(errors.New("query is empty"))
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 8
	}
	limit = min(limit, 20)
	x, err := index.Open(ctx, t.opts.Root)
	if err != nil {
		return toolError(err)
	}
	defer func() { _ = x.Close() }()
	hits, err := x.Search(ctx, index.SearchOptions{Query: in.Query, Limit: limit, Roles: in.Roles, Embedder: t.opts.Embedder, Model: t.opts.EmbedModel})
	if err != nil {
		return toolError(err)
	}
	for i := range hits {
		if r := []rune(hits[i].Text); len(r) > maxHitText {
			hits[i].Text = string(r[:maxHitText]) + " ..."
		}
	}
	if hits == nil {
		hits = []index.Hit{}
	}
	return result(map[string]any{"query": in.Query, "hits": hits})
}

// ReadInput is the input of read.
type ReadInput struct {
	Path     string `json:"path" jsonschema:"path of the file in the case folder, as given by search or files"`
	Offset   int    `json:"offset,omitempty" jsonschema:"character to start at; use next_offset of the last call"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"number of characters, up to 12000; default 6000"`
}

// textExts are the files read can return.
var textExts = []string{".txt", ".md", ".json", ".html", ".eml"}

func (t *tools) read(_ context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
	p, err := t.clean(in.Path)
	if err != nil {
		return toolError(err)
	}
	if !slices.Contains(textExts, strings.ToLower(path.Ext(p))) {
		return toolError(fmt.Errorf("%s is not a text file; use look for images, or read its text files (page-NNN.txt, ocr.txt, describe.txt)", p))
	}
	if p == casefile.ManifestFile || strings.HasSuffix(p, "/sync.json") {
		return toolError(errors.New("use the files tool for the manifest"))
	}
	data, err := os.ReadFile(filepath.Join(t.opts.Root, filepath.FromSlash(p)))
	if err != nil {
		return toolError(err)
	}
	text := []rune(string(data))
	limit := in.MaxChars
	if limit <= 0 {
		limit = 6000
	}
	limit = min(limit, 12000)
	start := min(max(in.Offset, 0), len(text))
	end := min(start+limit, len(text))
	out := map[string]any{"path": p, "offset": start, "total_chars": len(text), "text": string(text[start:end])}
	if end < len(text) {
		out["next_offset"] = end
	}
	if m, err := casefile.Load(t.opts.Root); err == nil {
		for _, d := range m.Derived {
			if d.Path == p {
				out["provenance"] = d
			}
		}
	}
	return result(out)
}

// clean checks that a path stays in the case folder and returns it as a slash path.
func (t *tools) clean(p string) (string, error) {
	p = strings.TrimSpace(filepath.ToSlash(p))
	if filepath.IsAbs(p) {
		if rel, err := filepath.Rel(t.opts.Root, p); err == nil {
			p = filepath.ToSlash(rel)
		}
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || path.IsAbs(c) {
		return "", fmt.Errorf("%s is not in the case folder", p)
	}
	return c, nil
}

// FilesInput is the input of files.
type FilesInput struct {
	Kind   string `json:"kind,omitempty" jsonschema:"keep one kind: original, body, attachment, inline, message, media, page-image, page-text, page-ocr, image-ocr, description"`
	Prefix string `json:"prefix,omitempty" jsonschema:"keep paths that start with this, such as extracted/my-email-1a2b3c4d/"`
	Limit  int    `json:"limit,omitempty" jsonschema:"number of files, up to 200; default 50"`
	Offset int    `json:"offset,omitempty" jsonschema:"files to skip, for the next page"`
}

// fileInfo is one file in the files result: its provenance in short.
type fileInfo struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Source     string `json:"source,omitempty"`
	From       string `json:"from,omitempty"`
	Page       int    `json:"page,omitempty"`
	Images     int    `json:"images,omitempty"`
	Attachment string `json:"attachment,omitempty"`
	Email      string `json:"email,omitempty"`
	Sender     string `json:"sender,omitempty"`
	Date       string `json:"date,omitempty"`
	Role       string `json:"role,omitempty"`
}

func (t *tools) files(_ context.Context, _ *mcp.CallToolRequest, in FilesInput) (*mcp.CallToolResult, any, error) {
	m, err := casefile.Load(t.opts.Root)
	if err != nil {
		return toolError(err)
	}
	var all []fileInfo
	for _, o := range m.Originals {
		all = append(all, fileInfo{Path: o.Path, Kind: "original", Role: o.Role})
	}
	for _, d := range m.Derived {
		f := fileInfo{Path: d.Path, Kind: d.Kind, Source: d.Source, From: d.From, Page: d.Page, Images: d.Images, Attachment: d.Attachment}
		if d.Message != nil {
			f.Email, f.Sender, f.Date = d.Message.Subject, d.Message.From, d.Message.Date
		}
		if d.Chat != nil {
			f.Sender, f.Date = d.Chat.Sender, d.Chat.Time
		}
		all = append(all, f)
	}
	all = slices.DeleteFunc(all, func(f fileInfo) bool {
		return (in.Kind != "" && f.Kind != in.Kind) || (in.Prefix != "" && !strings.HasPrefix(f.Path, in.Prefix))
	})
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	start := min(max(in.Offset, 0), len(all))
	end := min(start+limit, len(all))
	out := map[string]any{"total": len(all), "offset": start, "files": all[start:end]}
	if end < len(all) {
		out["next_offset"] = end
	}
	return result(out)
}

// TimelineInput is the input of timeline.
type TimelineInput struct {
	Since string   `json:"since,omitempty" jsonschema:"first day, YYYY-MM-DD"`
	Until string   `json:"until,omitempty" jsonschema:"last day, YYYY-MM-DD"`
	Kinds []string `json:"kinds,omitempty" jsonschema:"keep only: email, forwarded, chat"`
}

func (t *tools) timeline(_ context.Context, _ *mcp.CallToolRequest, in TimelineInput) (*mcp.CallToolResult, any, error) {
	events, err := timeline.Build(timeline.Options{Root: t.opts.Root, Since: in.Since, Until: in.Until, Kinds: in.Kinds})
	if err != nil {
		return toolError(err)
	}
	if events == nil {
		events = []timeline.Event{}
	}
	return result(map[string]any{"events": events})
}

// VerifyInput is the input of verify: nothing.
type VerifyInput struct{}

func (t *tools) verify(_ context.Context, _ *mcp.CallToolRequest, _ VerifyInput) (*mcp.CallToolResult, any, error) {
	res, err := verify.Run(t.opts.Root)
	if err != nil {
		return toolError(err)
	}
	return result(res)
}

// LookInput is the input of look.
type LookInput struct {
	Images   []string `json:"images" jsonschema:"image paths in the case folder, or <pdf path>#<page> for a PDF page"`
	Question string   `json:"question" jsonschema:"the question about the images"`
}

func (t *tools) look(ctx context.Context, _ *mcp.CallToolRequest, in LookInput) (*mcp.CallToolResult, any, error) {
	if len(in.Images) == 0 || strings.TrimSpace(in.Question) == "" {
		return toolError(errors.New("images and question are required"))
	}
	if len(in.Images) > 4 {
		return toolError(errors.New("give at most 4 images"))
	}
	res, err := vision.Look(ctx, t.opts.Root, t.opts.Vision, t.opts.VisionModel, in.Question, in.Images)
	if err != nil {
		return toolError(err)
	}
	return result(res)
}

// NoteInput is the input of note_write.
type NoteInput struct {
	Name      string `json:"name" jsonschema:"file name in notes/, such as status.md or drafts/reply.md"`
	Text      string `json:"text" jsonschema:"the Markdown text"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"replace a note with the same name"`
}

func (t *tools) noteWrite(_ context.Context, _ *mcp.CallToolRequest, in NoteInput) (*mcp.CallToolResult, any, error) {
	p, err := notes.Write(t.opts.Root, in.Name, in.Text, in.Overwrite)
	if err != nil {
		return toolError(err)
	}
	return result(map[string]any{"written": p, "next": "Run index to make the note searchable."})
}

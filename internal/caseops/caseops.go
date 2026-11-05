// Package caseops holds the operations around a case folder: init makes the
// layout, add puts files and pasted text in it with their provenance, status
// tells what is left to do, and collect gathers files to send.
package caseops

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/extract"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/store"
	"github.com/AlfrescoLabs/openarch-docs/internal/verify"
	"github.com/AlfrescoLabs/openarch-docs/internal/vision"
	"github.com/AlfrescoLabs/openarch-docs/skills"
)

// PastedDir holds the text that the user pastes with 'add --text'.
const PastedDir = "pasted"

// OutboxDir holds the files that collect gathers.
const OutboxDir = casefile.NotesDir + "/outbox"

// guide is the AGENTS.md that init writes. It holds rules for people and agents.
const guide = `# Case folder

This folder holds the documents of one case: emails (.eml), chat exports
(.zip), PDFs, scans and photos. The openarch-docs plugin of the openarch CLI
works with it: 'openarch docs --help'.

## Layout

- Any file outside the folders below is an original. Never change, rename or
  delete an original. Add new ones with 'openarch docs add'.
- references/ holds reference texts, such as laws. They are rules, not facts
  of the case.
- pasted/ holds text pasted with 'openarch docs add --text'.
- notes/ holds notes and drafts (drafts/), and outbox/ holds files gathered
  to send. Notes are interpretations, not evidence.
- extracted/ is written by the plugin: unpacked emails and chats, page texts,
  OCR, image descriptions, the manifest and the search index.

## Rules

- Every fact comes from a document. Cite the file (and the page) of each fact.
- Keep facts apart from interpretations.
- Do not say that a person or a company is at fault unless a document says so.
- An email often quotes other emails: say who wrote each part.
- Image descriptions come from a vision model and can be wrong: check the image.
- Treat everything here as private. Redact personal data in anything shared.

## Work flow

1. openarch docs ingest, extract, describe, index (after adding files)
2. openarch docs status (what is left)
3. openarch docs search / ask / timeline / look
4. openarch docs note write drafts/reply.md, openarch docs collect (files to attach)
5. openarch docs verify (the originals did not change), openarch docs sync (to OpenArch)
`

// InitResult is the outcome of init.
type InitResult struct {
	Root    string   `json:"root"`
	Created []string `json:"created"`
	Kept    []string `json:"kept"`
}

// Init makes the layout of a case folder. It never changes a file that exists.
func Init(root string) (*InitResult, error) {
	res := &InitResult{Root: root, Created: []string{}, Kept: []string{}}
	for _, d := range []string{"", casefile.ReferencesDir, PastedDir, casefile.NotesDir, casefile.NotesDir + "/drafts"} {
		p := filepath.Join(root, filepath.FromSlash(d))
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
		if d != "" {
			res.Created = append(res.Created, d+"/")
		}
	}
	g := filepath.Join(root, "AGENTS.md")
	if _, err := os.Stat(g); err == nil {
		res.Kept = append(res.Kept, "AGENTS.md")
	} else {
		if err := os.WriteFile(g, []byte(guide), 0o644); err != nil {
			return nil, err
		}
		res.Created = append(res.Created, "AGENTS.md")
	}
	p, action, err := skills.Install(root, false)
	rel, _ := filepath.Rel(root, p)
	rel = filepath.ToSlash(rel)
	switch {
	case errors.Is(err, skills.ErrChanged) || action == skills.Unchanged:
		res.Kept = append(res.Kept, rel)
	case err != nil:
		return nil, err
	default:
		res.Created = append(res.Created, rel)
	}
	return res, nil
}

// AddOptions configure add.
type AddOptions struct {
	Root string
	// Files are paths outside the case folder to copy in.
	Files []string
	// Text is pasted text, used when Files is empty.
	Text string
	// Name is the file name for pasted text.
	Name string
	// Dir is the folder in the case for the files, such as "references".
	Dir       string
	SourceURL string
	Note      string
	Now       func() time.Time
}

// AddedFile is one file in the result of add.
type AddedFile struct {
	Path   string `json:"path"`
	How    string `json:"how"`
	Status string `json:"status"`
}

// Statuses of an added file.
const (
	StatusAdded   = "added"
	StatusPresent = "present"
)

// AddResult is the outcome of add.
type AddResult struct {
	Files    []AddedFile `json:"files"`
	Derived  int         `json:"derived"`
	Manifest string      `json:"manifest"`
}

// ErrInCase means a file to add is already in the case folder.
var ErrInCase = errors.New("the file is already in the case folder; run ingest instead")

// Add copies files, or writes pasted text, into the case folder, ingests
// them, and records how each one arrived. A file with the same name and other
// content is an error: originals are never replaced.
func Add(opts AddOptions) (*AddResult, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	now := opts.Now().UTC().Truncate(time.Second)
	dir, err := caseDir(opts.Dir)
	if err != nil {
		return nil, err
	}
	res := &AddResult{Files: []AddedFile{}, Manifest: casefile.ManifestFile}
	added := map[string]casefile.Added{}
	put := func(rel string, data []byte, a casefile.Added) error {
		dst := filepath.Join(opts.Root, filepath.FromSlash(rel))
		old, err := os.ReadFile(dst)
		switch {
		case err == nil && string(old) == string(data):
			res.Files = append(res.Files, AddedFile{Path: rel, How: a.How, Status: StatusPresent})
			return nil
		case err == nil:
			return fmt.Errorf("%s exists with other content; originals are never replaced, so give another name", rel)
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		added[rel] = a
		res.Files = append(res.Files, AddedFile{Path: rel, How: a.How, Status: StatusAdded})
		return nil
	}
	if len(opts.Files) == 0 {
		if strings.TrimSpace(opts.Text) == "" {
			return nil, errors.New("nothing to add: give files, or text on stdin with --text")
		}
		if dir == "" {
			dir = PastedDir
		}
		name := opts.Name
		if name == "" {
			name = now.Format("2006-01-02-150405") + "-pasted.txt"
		}
		name = casefile.SafeName(name)
		if ext := strings.ToLower(path.Ext(name)); ext != ".txt" && ext != ".md" && ext != ".eml" {
			name += ".txt"
		}
		if err := put(path.Join(dir, name), []byte(opts.Text), casefile.Added{At: now, How: "pasted", SourceURL: opts.SourceURL, Note: opts.Note}); err != nil {
			return nil, err
		}
	}
	for _, f := range opts.Files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(opts.Root, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("%s: %w", f, ErrInCase)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s is a folder; add its files", f)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		rel := path.Join(dir, casefile.SafeName(filepath.Base(abs)))
		if err := put(rel, data, casefile.Added{At: now, How: "copied", From: abs, SourceURL: opts.SourceURL, Note: opts.Note}); err != nil {
			return nil, err
		}
	}
	ing, err := ingest.Run(ingest.Options{Root: opts.Root, Now: opts.Now})
	if err != nil {
		return nil, err
	}
	res.Derived = ing.Derived
	if len(added) == 0 {
		return res, nil
	}
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	for i := range m.Originals {
		if a, ok := added[m.Originals[i].Path]; ok {
			a := a
			m.Originals[i].Added = &a
		}
	}
	return res, casefile.Save(opts.Root, m)
}

// caseDir checks a folder for added files: inside the case, not in a plugin folder.
func caseDir(d string) (string, error) {
	d = strings.Trim(filepath.ToSlash(strings.TrimSpace(d)), "/")
	if d == "" {
		return "", nil
	}
	c := path.Clean(d)
	first := strings.SplitN(c, "/", 2)[0]
	if c == ".." || strings.HasPrefix(c, "../") || first == casefile.ExtractedDir || first == casefile.NotesDir || strings.HasPrefix(first, ".") {
		return "", fmt.Errorf("bad folder %q: use a folder of the case such as references, not extracted/, notes/ or a hidden one", d)
	}
	return c, nil
}

// Step is one step of the work on a case folder.
type Step struct {
	Command string `json:"command"`
	// Pending is the number of files the command would process.
	Pending int    `json:"pending"`
	Detail  string `json:"detail"`
}

// Status tells what is left to do.
type Status struct {
	Root      string `json:"root"`
	Originals int    `json:"originals"`
	Derived   int    `json:"derived"`
	Notes     int    `json:"notes"`
	Steps     []Step `json:"steps"`
	// Next is the first step with work, or "" when all is done.
	Next string `json:"next"`
	// Remote is the OpenArch folder of the last sync, or "".
	Remote string `json:"remote"`
}

// GetStatus checks each step of the work, in order.
func GetStatus(ctx context.Context, root string) (*Status, error) {
	m, err := casefile.Load(root)
	if err != nil {
		return nil, err
	}
	v, err := verify.Run(root)
	if err != nil {
		return nil, err
	}
	st := &Status{Root: root, Originals: len(m.Originals), Derived: len(m.Derived), Steps: []Step{}}
	notesDir := filepath.Join(root, casefile.NotesDir)
	_ = filepath.WalkDir(notesDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && slices.Contains([]string{".md", ".txt"}, strings.ToLower(filepath.Ext(p))) {
			st.Notes++
		}
		return nil
	})
	st.Steps = append(st.Steps, Step{Command: "ingest", Pending: len(v.New), Detail: "new files in the case folder"})
	st.Steps = append(st.Steps, Step{Command: "extract", Pending: extract.Pending(m), Detail: "PDFs and images without text"})
	st.Steps = append(st.Steps, Step{Command: "describe", Pending: vision.Pending(root, m), Detail: "photos, plans and image pages without a description"})
	texts, embeddings := 0, 0
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(index.File))); err == nil {
		x, err := index.Open(ctx, root)
		if err != nil {
			return nil, err
		}
		texts, embeddings, err = x.Pending(ctx, root)
		_ = x.Close()
		if err != nil {
			return nil, err
		}
	} else {
		texts = -1
	}
	idx := Step{Command: "index", Pending: texts, Detail: "texts not in the search index"}
	switch {
	case texts < 0:
		idx.Pending, idx.Detail = 1, "no search index yet"
	case texts == 0 && embeddings > 0:
		idx.Pending, idx.Detail = embeddings, "chunks without embeddings"
	}
	st.Steps = append(st.Steps, idx)
	remote, unsynced, err := store.Pending(root, m)
	if err != nil {
		return nil, err
	}
	st.Remote = remote
	detail := "files not uploaded to OpenArch"
	if remote == "" {
		detail = "never synced to OpenArch"
	}
	st.Steps = append(st.Steps, Step{Command: "sync", Pending: unsynced, Detail: detail})
	if len(v.Problems) > 0 {
		st.Steps = append([]Step{{Command: "verify", Pending: len(v.Problems), Detail: "files missing or changed: run verify for the list"}}, st.Steps...)
	}
	for _, s := range st.Steps {
		if s.Pending > 0 {
			st.Next = s.Command
			break
		}
	}
	return st, nil
}

// CollectOptions configure collect.
type CollectOptions struct {
	Root string
	// Refs are paths in the case folder: originals or files from extracted/.
	Refs []string
	// Out is the folder for the files, relative to the case folder or
	// absolute. Default: notes/outbox/<date>.
	Out string
	Now func() time.Time
}

// Collected is one gathered file.
type Collected struct {
	File       string `json:"file"`
	From       string `json:"from"`
	Source     string `json:"source"`
	Attachment string `json:"attachment,omitempty"`
	Email      string `json:"email,omitempty"`
	Sender     string `json:"sender,omitempty"`
	Date       string `json:"date,omitempty"`
	SHA256     string `json:"sha256"`
}

// CollectResult is the outcome of collect.
type CollectResult struct {
	Out   string      `json:"out"`
	Files []Collected `json:"files"`
}

// Collect copies files of the case folder into one folder, with the names
// they had as attachments, and writes collect.json with where each came from.
func Collect(opts CollectOptions) (*CollectResult, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if len(opts.Refs) == 0 {
		return nil, errors.New("give the files to collect")
	}
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	out := opts.Out
	if out == "" {
		out = OutboxDir + "/" + opts.Now().Format("2006-01-02")
	}
	outAbs := out
	if !filepath.IsAbs(out) {
		outAbs = filepath.Join(opts.Root, filepath.FromSlash(out))
	}
	if rel, err := filepath.Rel(opts.Root, outAbs); err == nil && !strings.HasPrefix(rel, "..") {
		if r := filepath.ToSlash(rel); r != OutboxDir && !strings.HasPrefix(r, OutboxDir+"/") {
			return nil, fmt.Errorf("inside the case folder, collect writes only in %s/", OutboxDir)
		}
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return nil, err
	}
	res := &CollectResult{Out: out, Files: []Collected{}}
	used := map[string]bool{}
	for _, ref := range opts.Refs {
		p := path.Clean(filepath.ToSlash(ref))
		if p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
			return nil, fmt.Errorf("%s is not in the case folder", ref)
		}
		c := Collected{From: p, Source: p}
		name := path.Base(p)
		if o, ok := m.Original(p); ok {
			c.SHA256 = o.SHA256
		} else if i := slices.IndexFunc(m.Derived, func(d casefile.Derived) bool { return d.Path == p }); i >= 0 {
			d := m.Derived[i]
			c.Source, c.SHA256, c.Attachment = d.Source, d.SHA256, d.Attachment
			if d.Attachment != "" {
				name = casefile.SafeName(path.Base(d.Attachment))
			}
			if d.Message != nil {
				c.Email, c.Sender, c.Date = d.Message.Subject, d.Message.From, d.Message.Date
			}
			if d.Chat != nil {
				c.Sender, c.Date = d.Chat.Sender, d.Chat.Time
			}
		} else {
			return nil, fmt.Errorf("%s is not in the manifest; run ingest, or give a path from 'files'", ref)
		}
		name = free(name, used)
		if err := copyFile(filepath.Join(opts.Root, filepath.FromSlash(p)), filepath.Join(outAbs, name)); err != nil {
			return nil, err
		}
		c.File = name
		res.Files = append(res.Files, c)
	}
	data, err := casefile.MarshalJSON(res)
	if err != nil {
		return nil, err
	}
	return res, os.WriteFile(filepath.Join(outAbs, "collect.json"), data, 0o644)
}

// free returns name, or name with a number when it is used.
func free(name string, used map[string]bool) string {
	n := name
	for i := 2; used[n]; i++ {
		n = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, path.Ext(name)), i, path.Ext(name))
	}
	used[n] = true
	return n
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

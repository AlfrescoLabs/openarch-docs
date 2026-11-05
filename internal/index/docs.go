package index

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/chat"
	"github.com/AlfrescoLabs/openarch-docs/internal/extract"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
)

// RoleNote marks notes and drafts: interpretations, not sources.
const RoleNote = "note"

// Chunk sizes, in characters. mxbai-embed-large reads 512 tokens; 1000
// characters of Spanish or French text are about 300.
const (
	chunkSize    = 1000
	chunkOverlap = 150
	// minLetters drops pieces with almost no text, such as OCR of a photo.
	minLetters = 20
)

// doc is one text file to index.
type doc struct {
	path   string // the text file, relative to the case folder
	sha    string
	source string // original it comes from; for notes, the note itself
	origin string // the PDF, image or email the text is from
	page   int
	kind   string
	role   string
	method string
	cite   string
	date   string
	sender string
}

// textKinds are the derived files that hold text.
var textKinds = []string{ingest.KindBody, ingest.KindChatJSON, extract.KindPageText, extract.KindPageOCR, extract.KindImageOCR}

// collect lists the texts of the case folder: derived texts from the
// manifest, text originals, and the notes.
func collect(root string, m *casefile.Manifest) ([]doc, error) {
	roles := map[string]string{}
	for _, o := range m.Originals {
		roles[o.Path] = o.Role
	}
	byPath := map[string]casefile.Derived{}
	for _, d := range m.Derived {
		byPath[d.Path] = d
	}
	var docs []doc
	for _, d := range m.Derived {
		isText := slices.Contains(textKinds, d.Kind) ||
			((d.Kind == ingest.KindAttachment || d.Kind == ingest.KindZipEntry) && d.MediaType == "text/plain")
		if !isText {
			continue
		}
		x := doc{path: d.Path, sha: d.SHA256, source: d.Source, origin: d.From, page: d.Page, kind: d.Kind,
			role: roles[d.Source], method: d.Method}
		if x.origin == "" {
			x.origin = d.Source
		}
		x.cite = citation(d, byPath)
		if d.Message != nil {
			x.date, x.sender = d.Message.Date, d.Message.From
		}
		docs = append(docs, x)
	}
	for _, o := range m.Originals {
		if o.Kind == casefile.KindText {
			docs = append(docs, doc{path: o.Path, sha: o.SHA256, source: o.Path, origin: o.Path, kind: "text", role: o.Role, cite: path.Base(o.Path)})
		}
	}
	notes, err := collectNotes(root)
	if err != nil {
		return nil, err
	}
	return append(docs, notes...), nil
}

// citation returns the short name a person uses to find the text: the
// attachment name or file name, the page, and the email subject for bodies.
func citation(d casefile.Derived, byPath map[string]casefile.Derived) string {
	name := path.Base(d.Source)
	if d.From != "" && d.From != d.Source {
		name = path.Base(d.From)
		if f, ok := byPath[d.From]; ok && f.Attachment != "" {
			name = path.Base(f.Attachment)
		}
	}
	switch {
	case d.Kind == ingest.KindBody && d.Message != nil && d.Message.Subject != "":
		name = fmt.Sprintf("%s, email %q", path.Base(d.Source), d.Message.Subject)
		if d.Message.Date != "" {
			name += " of " + d.Message.Date
		}
	case d.Kind == ingest.KindChatJSON:
		name = path.Base(d.Source) + ", chat"
	}
	if d.Page > 0 {
		name += fmt.Sprintf(" p.%d", d.Page)
	}
	if d.Method == extract.MethodOCR {
		name += " (OCR)"
	}
	return name
}

// collectNotes lists the Markdown and text files in notes/.
func collectNotes(root string) ([]doc, error) {
	dir := filepath.Join(root, casefile.NotesDir)
	var docs []doc
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		ext := strings.ToLower(filepath.Ext(p))
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") || (ext != ".md" && ext != ".txt") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		sum, _, err := casefile.HashFile(p)
		if err != nil {
			return err
		}
		docs = append(docs, doc{path: rel, sha: sum, source: rel, origin: rel, kind: RoleNote, role: RoleNote, cite: "note " + rel})
		return nil
	})
	return docs, err
}

// readText returns the text of a doc. Chat messages become one line each.
func readText(root string, d doc) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.path)))
	if err != nil {
		return "", err
	}
	if d.kind != ingest.KindChatJSON {
		return string(data), nil
	}
	var msgs []chat.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return "", fmt.Errorf("%s: %w", d.path, err)
	}
	var b strings.Builder
	for _, m := range msgs {
		t := m.ISO
		if t == "" {
			t = m.Time
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", t, m.Sender, m.Text)
	}
	return b.String(), nil
}

// chunks splits text into pieces of about chunkSize characters, at
// paragraph, line or sentence ends, with chunkOverlap characters of overlap.
func chunks(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []string
	rest := strings.TrimSpace(text)
	for rest != "" {
		if len([]rune(rest)) <= chunkSize {
			out = append(out, rest)
			break
		}
		r := []rune(rest)
		end := cut(r[:chunkSize])
		out = append(out, strings.TrimSpace(string(r[:end])))
		next := end - chunkOverlap
		if next <= 0 {
			next = end
		}
		// Start the overlap at a word.
		for next < end && !unicode.IsSpace(r[next-1]) {
			next++
		}
		rest = strings.TrimSpace(string(r[next:]))
	}
	return slices.DeleteFunc(out, func(c string) bool { return letters(c) < minLetters })
}

// cut returns where to end a piece: the last paragraph, line, sentence or
// word end in the second half of r, else len(r).
func cut(r []rune) int {
	s := string(r)
	half := len(s) / 2
	for _, sep := range []string{"\n\n", "\n", ". ", " "} {
		if i := strings.LastIndex(s, sep); i > half {
			return len([]rune(s[:i+len(sep)]))
		}
	}
	return len(r)
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

// Package ingest records the originals of a case folder and unpacks emails and
// chat exports into extracted/, with the provenance of every derived file.
package ingest

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/chat"
	"github.com/AlfrescoLabs/openarch-docs/internal/email"
)

// Kinds of a derived file.
const (
	KindBody       = "body"
	KindHTML       = "html"
	KindAttachment = "attachment"
	KindInline     = "inline"
	KindMessage    = "message"
	KindChat       = "chat"
	KindChatJSON   = "chat-messages"
	KindMedia      = "media"
	KindZipEntry   = "zip-entry"
)

// maxEntrySize limits one file read from a zip, against zip bombs.
const maxEntrySize = 512 << 20

// Options configure an ingest run.
type Options struct {
	// Root is the case folder.
	Root string
	// DryRun lists what would change and writes nothing.
	DryRun bool
	// Now returns the current time. Tests set it.
	Now func() time.Time
}

// File is one original in the result.
type File struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	Derived int    `json:"derived"`
}

// Statuses of an original in the result.
const (
	StatusAdded     = "added"
	StatusChanged   = "changed"
	StatusUnchanged = "unchanged"
	// StatusReparsed means a newer parser wrote the derived files again.
	StatusReparsed = "reparsed"
)

// ParserVersion grows when the email or zip parser writes different derived
// files. Originals ingested with an older parser are parsed again.
//
//	1: first release
//	2: Windows-1252 text labeled ISO-8859-1, HTML entities in plain text
const ParserVersion = 2

// ingestKinds are the kinds of derived files that ingest writes.
var ingestKinds = []string{KindBody, KindHTML, KindAttachment, KindInline, KindMessage, KindChat, KindChatJSON, KindMedia, KindZipEntry}

// Result is the outcome of an ingest run.
type Result struct {
	Root     string `json:"root"`
	DryRun   bool   `json:"dryRun"`
	Files    []File `json:"files"`
	Added    int    `json:"added"`
	Changed  int    `json:"changed"`
	Reparsed int    `json:"reparsed"`
	Derived  int    `json:"derived"`
	Manifest string `json:"manifest"`
}

// Run ingests the case folder.
func Run(opts Options) (*Result, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	paths, err := casefile.FindOriginals(opts.Root)
	if err != nil {
		return nil, err
	}
	res := &Result{Root: opts.Root, DryRun: opts.DryRun, Files: []File{}, Manifest: casefile.ManifestFile}
	for _, p := range paths {
		f, err := ingestFile(opts, m, p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		switch f.Status {
		case StatusAdded:
			res.Added++
		case StatusChanged:
			res.Changed++
		case StatusReparsed:
			res.Reparsed++
		}
		res.Derived += f.Derived
		res.Files = append(res.Files, f)
	}
	if opts.DryRun {
		return res, nil
	}
	_, err = os.Stat(filepath.Join(opts.Root, filepath.FromSlash(casefile.ManifestFile)))
	if res.Added == 0 && res.Changed == 0 && res.Reparsed == 0 && err == nil {
		return res, nil
	}
	return res, casefile.Save(opts.Root, m)
}

func ingestFile(opts Options, m *casefile.Manifest, p string) (File, error) {
	sum, size, err := casefile.HashFile(filepath.Join(opts.Root, filepath.FromSlash(p)))
	if err != nil {
		return File{}, err
	}
	f := File{Path: p, Kind: casefile.KindOf(p), Role: casefile.RoleOf(p), Status: StatusAdded}
	parsed := f.Kind == casefile.KindEmail || f.Kind == casefile.KindZip
	if old, ok := m.Original(p); ok {
		switch {
		case old.SHA256 != sum:
			f.Status = StatusChanged
		case old.Parser >= ParserVersion || !parsed:
			f.Status = StatusUnchanged
			return f, nil
		default:
			f.Status = StatusReparsed
		}
	}
	if opts.DryRun {
		return f, nil
	}
	switch f.Status {
	case StatusChanged:
		if err := m.RemoveDerived(opts.Root, func(d casefile.Derived) bool { return d.Source == p }); err != nil {
			return File{}, err
		}
	case StatusReparsed:
		// Keep the extract results: they are slow to make, and the files
		// they come from get the same paths again.
		if err := m.RemoveDerived(opts.Root, func(d casefile.Derived) bool { return d.Source == p && slices.Contains(ingestKinds, d.Kind) }); err != nil {
			return File{}, err
		}
	}
	// The provenance of 'add' stays when the file is ingested again.
	var added *casefile.Added
	if old, ok := m.Original(p); ok {
		added = old.Added
	}
	m.Originals = slices.DeleteFunc(m.Originals, func(o casefile.Original) bool { return o.Path == p })
	m.Originals = append(m.Originals, casefile.Original{
		Path: p, SHA256: sum, Size: size, Kind: f.Kind, Role: f.Role, IngestedAt: opts.Now().UTC().Truncate(time.Second), Parser: ParserVersion, Added: added,
	})
	w := &writer{root: opts.Root, manifest: m, source: p,
		dir: casefile.DerivedDir(p, sum)}
	data, err := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(p)))
	if err != nil {
		return File{}, err
	}
	switch f.Kind {
	case casefile.KindEmail:
		msg, err := email.Parse(bytes.NewReader(data))
		if err != nil {
			return File{}, err
		}
		err = w.email(w.dir, msg, nil)
		if err != nil {
			return File{}, err
		}
	case casefile.KindZip:
		if err := w.zip(data); err != nil {
			return File{}, err
		}
	}
	f.Derived = w.count
	if f.Status == StatusReparsed {
		// Drop extract results of files that the new parser no longer writes.
		kept := map[string]bool{p: true}
		for _, d := range m.Derived {
			kept[d.Path] = true
		}
		if err := m.RemoveDerived(opts.Root, func(d casefile.Derived) bool { return d.Source == p && d.From != "" && !kept[d.From] }); err != nil {
			return File{}, err
		}
	}
	return f, nil
}

// writer writes the derived files of one original.
type writer struct {
	root     string
	manifest *casefile.Manifest
	source   string
	dir      string
	count    int
}

// write stores data at a free path near name and adds it to the manifest.
func (w *writer) write(name string, data []byte, d casefile.Derived) error {
	d.Source = w.source
	if _, err := w.manifest.WriteDerived(w.root, name, data, d); err != nil {
		return err
	}
	w.count++
	return nil
}

// email writes the body, the attachments and the nested messages of msg
// into dir. chain lists the emails around msg, outermost first.
func (w *writer) email(dir string, msg *email.Message, chain []casefile.MessageInfo) error {
	info := msg.Info
	var chainCopy []casefile.MessageInfo
	if len(chain) > 0 {
		chainCopy = slices.Clone(chain)
	}
	if err := w.write(path.Join(dir, "message.txt"), []byte(renderMessage(msg)), casefile.Derived{
		Kind: KindBody, MediaType: "text/plain", Message: &info, Chain: chainCopy, Forwarded: email.Forwarded(msg.Text),
	}); err != nil {
		return err
	}
	if msg.HTML != "" {
		if err := w.write(path.Join(dir, "message.html"), []byte(msg.HTML), casefile.Derived{
			Kind: KindHTML, MediaType: "text/html", Message: &info, Chain: chainCopy,
		}); err != nil {
			return err
		}
	}
	for _, p := range msg.Parts {
		kind, folder := KindAttachment, "attachments"
		if p.Inline {
			kind, folder = KindInline, "inline"
		}
		name := p.Filename
		if name == "" {
			name = "part-" + p.Number + extensionFor(p.MediaType)
		}
		if err := w.write(path.Join(dir, folder, casefile.SafeName(name)), p.Data, casefile.Derived{
			Kind: kind, MediaType: p.MediaType, Part: p.Number, Attachment: p.Filename, Message: &info, Chain: chainCopy,
		}); err != nil {
			return err
		}
	}
	inner := append(slices.Clone(chain), info)
	for i, n := range msg.Nested {
		sub := path.Join(dir, fmt.Sprintf("message-%d", i+1))
		nestedInfo := n.Message.Info
		if err := w.write(path.Join(sub, "message.eml"), n.Raw, casefile.Derived{
			Kind: KindMessage, MediaType: "message/rfc822", Part: n.Number, Message: &nestedInfo, Chain: slices.Clone(inner),
		}); err != nil {
			return err
		}
		if err := w.email(sub, n.Message, inner); err != nil {
			return err
		}
	}
	return nil
}

// renderMessage returns the headers and the body text of msg.
func renderMessage(msg *email.Message) string {
	var b strings.Builder
	for _, h := range []struct{ k, v string }{
		{"From", msg.Info.From}, {"To", msg.Info.To}, {"Cc", msg.Info.Cc}, {"Date", msg.Info.Date}, {"Subject", msg.Info.Subject},
	} {
		if h.v != "" {
			fmt.Fprintf(&b, "%s: %s\n", h.k, h.v)
		}
	}
	b.WriteString("\n")
	b.WriteString(msg.Text)
	b.WriteString("\n")
	return b.String()
}

func extensionFor(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	default:
		return ".bin"
	}
}

// zip unpacks a zip original. A WhatsApp export gives the chat text, the
// messages as JSON and the media, each linked to the message that sent it.
// Any other zip gives its files.
func (w *writer) zip(data []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	files := map[string][]byte{}
	var names []string
	chatName := ""
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		b, err := readEntry(zf)
		if err != nil {
			return fmt.Errorf("%s: %w", zf.Name, err)
		}
		files[zf.Name] = b
		names = append(names, zf.Name)
		if chatName == "" && chat.IsChatFile(zf.Name) {
			chatName = zf.Name
		}
	}
	if chatName == "" {
		for _, n := range names {
			if err := w.write(path.Join(w.dir, "files", safeZipPath(n)), files[n], casefile.Derived{Kind: KindZipEntry, Attachment: n}); err != nil {
				return err
			}
		}
		return nil
	}
	msgs := chat.Parse(string(files[chatName]))
	if err := w.write(path.Join(w.dir, "chat.txt"), files[chatName], casefile.Derived{Kind: KindChat, MediaType: "text/plain", Attachment: chatName}); err != nil {
		return err
	}
	js, err := casefile.MarshalJSON(msgs)
	if err != nil {
		return err
	}
	if err := w.write(path.Join(w.dir, "messages.json"), js, casefile.Derived{Kind: KindChatJSON, MediaType: "application/json", Attachment: chatName}); err != nil {
		return err
	}
	sentBy := map[string]chat.Message{}
	for _, m := range msgs {
		if m.Attachment != "" {
			sentBy[m.Attachment] = m
		}
	}
	for _, n := range names {
		if n == chatName {
			continue
		}
		d := casefile.Derived{Kind: KindMedia, Attachment: n}
		if m, ok := sentBy[path.Base(n)]; ok {
			t := m.ISO
			if t == "" {
				t = m.Time
			}
			d.Chat = &casefile.ChatInfo{Sender: m.Sender, Time: t}
		}
		if err := w.write(path.Join(w.dir, "media", casefile.SafeName(n)), files[n], d); err != nil {
			return err
		}
	}
	return nil
}

func readEntry(zf *zip.File) ([]byte, error) {
	if zf.UncompressedSize64 > maxEntrySize {
		return nil, fmt.Errorf("file is larger than %d MB", maxEntrySize>>20)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, maxEntrySize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxEntrySize {
		return nil, fmt.Errorf("file is larger than %d MB", maxEntrySize>>20)
	}
	return b, nil
}

// safeZipPath keeps the folders of a zip entry but drops "..", absolute
// paths and unsafe characters, so the file stays inside the target folder.
func safeZipPath(name string) string {
	var parts []string
	for _, p := range strings.Split(strings.ReplaceAll(name, "\\", "/"), "/") {
		if p == "" || p == "." || p == ".." {
			continue
		}
		parts = append(parts, casefile.SafeName(p))
	}
	if len(parts) == 0 {
		return "file"
	}
	return strings.Join(parts, "/")
}

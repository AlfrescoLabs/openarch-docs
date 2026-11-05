// Package casefile reads and writes the case folder: the original files, the
// derived files in extracted/, and the manifest that links them.
package casefile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// ExtractedDir holds every file the plugin writes, except notes.
	ExtractedDir = "extracted"
	// NotesDir holds notes and drafts. They are interpretations, not sources.
	NotesDir = "notes"
	// ReferencesDir holds reference texts the user adds, such as laws.
	ReferencesDir = "references"
	// ManifestFile is the manifest path, relative to the case folder.
	ManifestFile = ExtractedDir + "/manifest.json"
	// SchemaVersion is the manifest version this release writes.
	SchemaVersion = 1
)

// Roles of an original file.
const (
	RoleCase      = "case"
	RoleReference = "reference"
)

// Kinds of an original file.
const (
	KindEmail = "email"
	KindZip   = "zip"
	KindPDF   = "pdf"
	KindImage = "image"
	KindText  = "text"
	KindOther = "other"
)

// skipNames are files that are never case documents.
var skipNames = []string{".DS_Store", "Thumbs.db", "desktop.ini", "AGENTS.md", "CLAUDE.md"}

// Manifest lists the originals and the files derived from them.
type Manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Originals     []Original `json:"originals"`
	Derived       []Derived  `json:"derived"`
}

// Original is a file the user put in the case folder. The plugin never changes it.
type Original struct {
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	Kind       string    `json:"kind"`
	Role       string    `json:"role"`
	IngestedAt time.Time `json:"ingestedAt"`
	// Parser is the version of the email and zip parser that wrote the derived files.
	Parser int `json:"parser,omitempty"`
	// Added tells how 'add' put the file in the case folder.
	Added *Added `json:"added,omitempty"`
}

// Added is the provenance of a file that 'add' put in the case folder.
type Added struct {
	At time.Time `json:"at"`
	// How is "copied" or "pasted".
	How string `json:"how"`
	// From is the path the file was copied from.
	From string `json:"from,omitempty"`
	// SourceURL is where the user got the file, such as the page of a law.
	SourceURL string `json:"sourceUrl,omitempty"`
	Note      string `json:"note,omitempty"`
}

// Derived is a file the plugin wrote from an original.
type Derived struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	MediaType string `json:"mediaType,omitempty"`
	// Part is the MIME part number in the source email, such as "2.1".
	Part string `json:"part,omitempty"`
	// Attachment is the file name the sender gave the attachment.
	Attachment string `json:"attachment,omitempty"`
	// Message is the email that holds this file.
	Message *MessageInfo `json:"message,omitempty"`
	// Chain lists the emails around Message, outermost first, for nested messages.
	Chain []MessageInfo `json:"chain,omitempty"`
	// Forwarded lists forwarded messages quoted in the body text, in text order.
	Forwarded []MessageInfo `json:"forwarded,omitempty"`
	// Chat is the chat message that sent this file.
	Chat *ChatInfo `json:"chat,omitempty"`
	// From is the file this one was made from, when it is not Source itself,
	// such as the PDF attachment of an email for the text of one page.
	From string `json:"from,omitempty"`
	// Page is the page number in From or Source, starting at 1.
	Page int `json:"page,omitempty"`
	// Method is how the text was made: "text-layer" or "ocr".
	Method string `json:"method,omitempty"`
	// Model is the model that wrote the file, for descriptions.
	Model string `json:"model,omitempty"`
	// Languages are the OCR languages, as ISO 639-1 codes.
	Languages []string `json:"languages,omitempty"`
	// Images is the number of large images on the page, such as photos or plans.
	Images int `json:"images,omitempty"`
}

// MessageInfo holds the headers of an email.
type MessageInfo struct {
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Cc        string `json:"cc,omitempty"`
	Date      string `json:"date,omitempty"`
	Subject   string `json:"subject,omitempty"`
	MessageID string `json:"messageId,omitempty"`
}

// ChatInfo identifies a chat message.
type ChatInfo struct {
	Sender string `json:"sender"`
	Time   string `json:"time"`
}

// Load reads the manifest of the case folder root. A missing manifest is an empty one.
func Load(root string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ManifestFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return &Manifest{SchemaVersion: SchemaVersion, Originals: []Original{}, Derived: []Derived{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if m.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("%s has schemaVersion %d; this release reads up to %d", ManifestFile, m.SchemaVersion, SchemaVersion)
	}
	if m.Originals == nil {
		m.Originals = []Original{}
	}
	if m.Derived == nil {
		m.Derived = []Derived{}
	}
	return &m, nil
}

// Save writes the manifest, sorted by path. It writes a temporary file first,
// so a crash never leaves half a manifest.
func Save(root string, m *Manifest) error {
	m.SchemaVersion = SchemaVersion
	slices.SortFunc(m.Originals, func(a, b Original) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(m.Derived, func(a, b Derived) int { return strings.Compare(a.Path, b.Path) })
	data, err := MarshalJSON(m)
	if err != nil {
		return err
	}
	dst := filepath.Join(root, filepath.FromSlash(ManifestFile))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// MarshalJSON returns v as indented JSON with a final newline. It keeps <, >
// and & as they are, so email addresses stay readable.
func MarshalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Original returns the original with path p.
func (m *Manifest) Original(p string) (Original, bool) {
	i := slices.IndexFunc(m.Originals, func(o Original) bool { return o.Path == p })
	if i < 0 {
		return Original{}, false
	}
	return m.Originals[i], true
}

// DerivedDir returns the folder for the derived files of an original:
// extracted/<name>-<first 8 characters of the SHA-256>.
func DerivedDir(original, sha string) string {
	name := strings.TrimSuffix(path.Base(original), path.Ext(original))
	return path.Join(ExtractedDir, Slug(name, 40)+"-"+sha[:8])
}

// WriteDerived writes data to name, or to name with a number before the
// extension when a derived file already has that path, and adds it to the
// manifest. It fills the path, checksum and size of d and returns the path.
func (m *Manifest) WriteDerived(root, name string, data []byte, d Derived) (string, error) {
	p := m.freePath(name)
	dst := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	d.Path, d.SHA256, d.Size = p, HashBytes(data), int64(len(data))
	m.Derived = append(m.Derived, d)
	return p, nil
}

func (m *Manifest) freePath(name string) string {
	taken := func(p string) bool {
		return slices.ContainsFunc(m.Derived, func(d Derived) bool { return d.Path == p })
	}
	if !taken(name) {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		if p := fmt.Sprintf("%s-%d%s", base, i, ext); !taken(p) {
			return p
		}
	}
}

// RemoveDerived deletes the derived files that drop returns true for, and
// drops them from the manifest.
func (m *Manifest) RemoveDerived(root string, drop func(Derived) bool) error {
	keep := m.Derived[:0]
	for _, d := range m.Derived {
		if !drop(d) {
			keep = append(keep, d)
			continue
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(d.Path))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	m.Derived = keep
	return nil
}

// FindOriginals lists the original files under root, as slash paths relative
// to root. It skips extracted/, notes/, hidden folders and files that are
// never case documents.
func FindOriginals(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (rel == ExtractedDir || rel == NotesDir || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") || slices.Contains(skipNames, d.Name()) {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	slices.Sort(paths)
	return paths, err
}

// RoleOf returns the role of an original path.
func RoleOf(p string) string {
	if strings.HasPrefix(p, ReferencesDir+"/") {
		return RoleReference
	}
	return RoleCase
}

// KindOf returns the kind of a file from its name.
func KindOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".eml":
		return KindEmail
	case ".zip":
		return KindZip
	case ".pdf":
		return KindPDF
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".tif", ".tiff", ".bmp":
		return KindImage
	case ".txt", ".md":
		return KindText
	default:
		return KindOther
	}
}

// HashFile returns the SHA-256 (hex) and the size of a file.
func HashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// HashBytes returns the SHA-256 (hex) of data.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Slug turns a name into a short ASCII folder name: accents removed, other
// characters replaced with '-', at most max characters.
func Slug(name string, maxLen int) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(name) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(unicode.ToLower(r))
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > maxLen {
		s = strings.TrimRight(s[:maxLen], "-")
	}
	if s == "" {
		s = "file"
	}
	return s
}

// SafeName turns a name from an email or an archive into a file name that
// stays in its folder: no path separators, no leading dots, no control characters.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if name == "" {
		name = "file"
	}
	return name
}

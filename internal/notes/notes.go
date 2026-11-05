// Package notes writes notes and drafts in the notes/ folder of a case. It is
// the only place where agents may write.
package notes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
)

// MaxSize is the largest note, in bytes.
const MaxSize = 256 << 10

// validName is a note name: letters, digits, dots, dashes and underscores,
// with at most one folder, such as "drafts/reply-2026-11-05.md".
var validName = regexp.MustCompile(`^([A-Za-z0-9_-]+/)?[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ErrExists means a note with the name exists and overwrite was not asked.
var ErrExists = errors.New("the note exists")

// Path returns the path of a note in the case folder, such as
// "notes/drafts/reply.md". A name without .md or .txt gets .md.
func Path(name string) (string, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), casefile.NotesDir+"/")
	if !validName.MatchString(name) || strings.Contains(name, "..") {
		return "", fmt.Errorf("bad note name %q: use letters, digits, '.', '-' and '_', and at most one folder, such as drafts/reply.md", name)
	}
	if ext := strings.ToLower(path.Ext(name)); ext != ".md" && ext != ".txt" {
		name += ".md"
	}
	return casefile.NotesDir + "/" + name, nil
}

// Write writes a note and returns its path in the case folder.
func Write(root, name, text string, overwrite bool) (string, error) {
	p, err := Path(name)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("the note is empty")
	}
	if len(text) > MaxSize {
		return "", fmt.Errorf("the note is larger than %d KB", MaxSize>>10)
	}
	dst := filepath.Join(root, filepath.FromSlash(p))
	if _, err := os.Stat(dst); err == nil && !overwrite {
		return "", fmt.Errorf("%s: %w; choose another name, or overwrite", p, ErrExists)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return p, os.WriteFile(dst, []byte(text), 0o644)
}

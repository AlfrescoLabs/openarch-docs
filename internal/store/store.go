// Package store keeps the case folder in OpenArch, the store of record:
// Sync uploads the originals and derived files, each with its manifest
// entry in sys_description; Restore rebuilds the case folder from OpenArch.
package store

import (
	"bytes"
	"context"
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

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/openarch"
)

// StateFile records what is in OpenArch, so a sync uploads only changes.
const StateFile = casefile.ExtractedDir + "/sync.json"

// descPrefix starts the sys_description of every document the plugin uploads.
const descPrefix = "openarch-docs/1 "

// Roles of an uploaded file.
const (
	RoleOriginal = "original"
	RoleDerived  = "derived"
)

// description is the sys_description of an uploaded file.
type description struct {
	Role     string             `json:"role"`
	Original *casefile.Original `json:"original,omitempty"`
	Derived  *casefile.Derived  `json:"derived,omitempty"`
}

// State is the content of sync.json.
type State struct {
	Remote  string               `json:"remote"`
	Folders map[string]string    `json:"folders"`
	Files   map[string]FileState `json:"files"`
}

// FileState is one uploaded file.
type FileState struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// DefaultRemote returns the OpenArch folder of a case folder: /cases/<name>.
func DefaultRemote(root string) string {
	return "/cases/" + casefile.Slug(filepath.Base(root), 60)
}

// SyncOptions configure a sync.
type SyncOptions struct {
	Root   string
	Remote string
	Client openarch.Client
	// OriginalsOnly skips the derived files.
	OriginalsOnly bool
	DryRun        bool
	Progress      io.Writer
}

// SyncResult is the outcome of a sync.
type SyncResult struct {
	Remote    string   `json:"remote"`
	DryRun    bool     `json:"dryRun"`
	Uploaded  []string `json:"uploaded"`
	Unchanged int      `json:"unchanged"`
	Folders   int      `json:"foldersCreated"`
}

// item is one local file and its place in OpenArch.
type item struct {
	local  string // path in the case folder
	remote string // path in OpenArch
	sha    string
	desc   description
}

func items(m *casefile.Manifest, remote string, originalsOnly bool) []item {
	var out []item
	for i := range m.Originals {
		o := m.Originals[i]
		out = append(out, item{local: o.Path, remote: remote + "/originals/" + o.Path, sha: o.SHA256, desc: description{Role: RoleOriginal, Original: &o}})
	}
	if originalsOnly {
		return out
	}
	for i := range m.Derived {
		d := m.Derived[i]
		out = append(out, item{local: d.Path, remote: remote + "/" + d.Path, sha: d.SHA256, desc: description{Role: RoleDerived, Derived: &d}})
	}
	return out
}

// Sync uploads the files that are new or changed since the last sync.
func Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	st, err := loadState(opts.Root)
	if err != nil {
		return nil, err
	}
	if st.Remote != opts.Remote {
		st = &State{Remote: opts.Remote, Folders: map[string]string{}, Files: map[string]FileState{}}
	}
	res := &SyncResult{Remote: opts.Remote, DryRun: opts.DryRun, Uploaded: []string{}}
	f := &folders{client: opts.Client, state: st, dryRun: opts.DryRun}
	save := func() error {
		if opts.DryRun {
			return nil
		}
		return saveState(opts.Root, st)
	}
	for n, it := range items(m, opts.Remote, opts.OriginalsOnly) {
		if old, ok := st.Files[it.local]; ok && old.SHA256 == it.sha {
			res.Unchanged++
			continue
		}
		res.Uploaded = append(res.Uploaded, it.local)
		if opts.DryRun {
			continue
		}
		parent := path.Dir(it.remote)
		if err := f.ensure(ctx, parent); err != nil {
			_ = save()
			return nil, err
		}
		desc, err := compactJSON(it.desc)
		if err != nil {
			return nil, err
		}
		if opts.Progress != nil {
			_, _ = fmt.Fprintf(opts.Progress, "Uploading %s\n", it.local)
		}
		doc, err := opts.Client.Upload(ctx, filepath.Join(opts.Root, filepath.FromSlash(it.local)), parent, path.Base(it.remote), descPrefix+string(desc))
		if err != nil {
			_ = save()
			return nil, fmt.Errorf("upload %s: %w", it.local, err)
		}
		st.Files[it.local] = FileState{ID: doc.ID, SHA256: it.sha}
		if n%20 == 19 {
			if err := save(); err != nil {
				return nil, err
			}
		}
	}
	res.Folders = f.created
	return res, save()
}

// compactJSON returns v as one line of JSON, with <, > and & kept as they
// are, so email addresses stay readable in OpenArch.
func compactJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// Pending returns the remote of the last sync, or "" when the case was never
// synced, and the number of files that a sync would upload.
func Pending(root string, m *casefile.Manifest) (string, int, error) {
	st, err := loadState(root)
	if err != nil {
		return "", 0, err
	}
	remote := st.Remote
	if remote == "" {
		remote = DefaultRemote(root)
	}
	n := 0
	for _, it := range items(m, remote, false) {
		if old, ok := st.Files[it.local]; !ok || old.SHA256 != it.sha {
			n++
		}
	}
	return st.Remote, n, nil
}

// folders creates the folders of a path in OpenArch, like mkdir -p.
type folders struct {
	client  openarch.Client
	state   *State
	dryRun  bool
	created int
}

func (f *folders) ensure(ctx context.Context, p string) error {
	if p == "/" || p == "." || p == "" {
		return nil
	}
	if _, ok := f.state.Folders[p]; ok {
		return nil
	}
	doc, err := f.client.Get(ctx, p)
	switch {
	case err == nil && !doc.Folderish:
		return fmt.Errorf("%s exists in OpenArch and is not a folder", p)
	case err == nil:
	case openarch.IsNotFound(err):
		if err := f.ensure(ctx, path.Dir(p)); err != nil {
			return err
		}
		if doc, err = f.client.CreateFolder(ctx, path.Dir(p), path.Base(p)); err != nil {
			return fmt.Errorf("create folder %s: %w", p, err)
		}
		f.created++
	default:
		return err
	}
	f.state.Folders[p] = doc.ID
	return nil
}

func loadState(root string) (*State, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(StateFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Folders: map[string]string{}, Files: map[string]FileState{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", StateFile, err)
	}
	if st.Folders == nil {
		st.Folders = map[string]string{}
	}
	if st.Files == nil {
		st.Files = map[string]FileState{}
	}
	return &st, nil
}

func saveState(root string, st *State) error {
	data, err := casefile.MarshalJSON(st)
	if err != nil {
		return err
	}
	dst := filepath.Join(root, filepath.FromSlash(StateFile))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// RestoreOptions configure a restore.
type RestoreOptions struct {
	Root     string
	Remote   string
	Client   openarch.Client
	Progress io.Writer
}

// RestoreResult is the outcome of a restore.
type RestoreResult struct {
	Remote     string `json:"remote"`
	Downloaded int    `json:"downloaded"`
	Kept       int    `json:"kept"`
	// Conflicts are local originals that differ from OpenArch. They are
	// never overwritten.
	Conflicts []string `json:"conflicts"`
	// Corrupt are files whose download does not match the SHA-256 in their
	// description. They are not written and get no manifest entry.
	Corrupt []string `json:"corrupt"`
	// Skipped counts documents in the folder that the plugin did not upload.
	Skipped  int    `json:"skipped"`
	Manifest string `json:"manifest"`
}

// remoteFile is a file document the plugin uploaded.
type remoteFile struct {
	doc  openarch.Doc
	desc description
}

// Restore downloads the case folder from OpenArch and adds the entries in the
// descriptions to the manifest. Files that already match are kept, and local
// originals that differ are never overwritten.
func Restore(ctx context.Context, opts RestoreOptions) (*RestoreResult, error) {
	var files []remoteFile
	res := &RestoreResult{Remote: opts.Remote, Conflicts: []string{}, Corrupt: []string{}, Manifest: casefile.ManifestFile}
	var walk func(folder string) error
	walk = func(folder string) error {
		docs, err := opts.Client.List(ctx, folder)
		if err != nil {
			return err
		}
		for _, d := range docs {
			if d.Folderish {
				if err := walk(d.Path); err != nil {
					return err
				}
				continue
			}
			raw, ok := strings.CutPrefix(d.Description, descPrefix)
			var desc description
			if !ok || json.Unmarshal([]byte(raw), &desc) != nil || (desc.Original == nil && desc.Derived == nil) {
				res.Skipped++
				continue
			}
			files = append(files, remoteFile{doc: d, desc: desc})
		}
		return nil
	}
	if err := walk(opts.Remote); err != nil {
		return nil, err
	}
	// Entries from OpenArch join the local manifest; local entries of files
	// that were never synced stay.
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	st, err := loadState(opts.Root)
	if err != nil {
		return nil, err
	}
	if st.Remote != opts.Remote {
		st = &State{Remote: opts.Remote, Folders: map[string]string{}, Files: map[string]FileState{}}
	}
	for _, f := range files {
		local, sha := "", ""
		if f.desc.Original != nil {
			local, sha = f.desc.Original.Path, f.desc.Original.SHA256
		} else {
			local, sha = f.desc.Derived.Path, f.desc.Derived.SHA256
		}
		if !safeLocal(local) {
			res.Skipped++
			continue
		}
		dst := filepath.Join(opts.Root, filepath.FromSlash(local))
		got, _, err := casefile.HashFile(dst)
		switch {
		case err == nil && got == sha:
			res.Kept++
		case err == nil && f.desc.Original != nil:
			res.Conflicts = append(res.Conflicts, local)
			continue
		default:
			if opts.Progress != nil {
				_, _ = fmt.Fprintf(opts.Progress, "Downloading %s\n", local)
			}
			err := download(ctx, opts.Client, f.doc.ID, dst, sha)
			if errors.Is(err, errHashMismatch) {
				res.Corrupt = append(res.Corrupt, local)
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("download %s: %w", local, err)
			}
			res.Downloaded++
		}
		if f.desc.Original != nil {
			m.Originals = slices.DeleteFunc(m.Originals, func(o casefile.Original) bool { return o.Path == local })
			m.Originals = append(m.Originals, *f.desc.Original)
		} else {
			m.Derived = slices.DeleteFunc(m.Derived, func(d casefile.Derived) bool { return d.Path == local })
			m.Derived = append(m.Derived, *f.desc.Derived)
		}
		st.Files[local] = FileState{ID: f.doc.ID, SHA256: sha}
	}
	slices.Sort(res.Conflicts)
	slices.Sort(res.Corrupt)
	if err := casefile.Save(opts.Root, m); err != nil {
		return nil, err
	}
	return res, saveState(opts.Root, st)
}

// safeLocal reports whether a path from OpenArch stays inside the case folder.
func safeLocal(p string) bool {
	c := path.Clean(p)
	return c == p && c != "." && !path.IsAbs(c) && c != ".." && !strings.HasPrefix(c, "../")
}

// errHashMismatch means a downloaded file does not match its SHA-256.
var errHashMismatch = errors.New("download does not match its SHA-256")

// download writes the file of a document to dst through a temporary file. The
// file only replaces dst when its SHA-256 is sha.
func download(ctx context.Context, c openarch.Client, id, dst, sha string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".download"
	if err := c.Download(ctx, id, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	got, _, err := casefile.HashFile(tmp)
	if err == nil && got != sha {
		err = errHashMismatch
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

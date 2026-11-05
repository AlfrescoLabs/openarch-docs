package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/openarch"
)

// fakeOA is an in-memory OpenArch: documents by path.
type fakeOA struct {
	docs    map[string]openarch.Doc
	content map[string][]byte // by id
	uploads int
	seq     int
}

func newFakeOA() *fakeOA {
	return &fakeOA{docs: map[string]openarch.Doc{"/": {ID: "root", Path: "/", Folderish: true}}, content: map[string][]byte{}}
}

func (f *fakeOA) Get(_ context.Context, ref string) (openarch.Doc, error) {
	if d, ok := f.docs[ref]; ok {
		return d, nil
	}
	return openarch.Doc{}, &openarch.Error{Exit: 5, Code: "remote", Message: "get document " + ref + ": not found: " + ref}
}

func (f *fakeOA) CreateFolder(_ context.Context, parent, name string) (openarch.Doc, error) {
	if _, ok := f.docs[parent]; !ok {
		return openarch.Doc{}, fmt.Errorf("no parent %s", parent)
	}
	f.seq++
	d := openarch.Doc{ID: fmt.Sprintf("f%d", f.seq), Path: path.Join(parent, name), Name: name, Folderish: true}
	f.docs[d.Path] = d
	return d, nil
}

func (f *fakeOA) Upload(_ context.Context, file, parent, name, description string) (openarch.Doc, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return openarch.Doc{}, err
	}
	f.uploads++
	p := path.Join(parent, name)
	d, ok := f.docs[p]
	if !ok {
		f.seq++
		d = openarch.Doc{ID: fmt.Sprintf("d%d", f.seq), Path: p, Name: name, Type: "SysFile"}
	}
	d.Description = description
	f.docs[p] = d
	f.content[d.ID] = data
	return d, nil
}

func (f *fakeOA) List(_ context.Context, folder string) ([]openarch.Doc, error) {
	var out []openarch.Doc
	for p, d := range f.docs {
		if p != "/" && path.Dir(p) == folder {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (f *fakeOA) Download(_ context.Context, ref, out string) error {
	return os.WriteFile(out, f.content[ref], 0o644)
}

func newCase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for n, data := range map[string]string{
		"aviso.eml":        "From: Ana <ana@example.com>\nSubject: Plano\nContent-Type: multipart/mixed; boundary=b\n\n--b\nContent-Type: text/plain\n\nVa el plano.\n--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=plano.pdf\n\n%PDF\n--b--\n",
		"fotos/foto.jpg":   "jpeg",
		"references/l.txt": "ley",
	} {
		p := filepath.Join(root, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSyncAndRestore(t *testing.T) {
	root := newCase(t)
	oa := newFakeOA()
	ctx := context.Background()
	opts := SyncOptions{Root: root, Remote: "/cases/c", Client: oa}

	dry := opts
	dry.DryRun = true
	res, err := Sync(ctx, dry)
	if err != nil || len(res.Uploaded) != 5 || oa.uploads != 0 {
		t.Fatalf("dry run = %+v, %v, uploads %d", res, err, oa.uploads)
	}

	res, err = Sync(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	// 3 originals, message.txt and the PDF attachment.
	if len(res.Uploaded) != 5 || oa.uploads != 5 {
		t.Fatalf("sync = %+v, uploads %d", res, oa.uploads)
	}
	d, err := oa.Get(ctx, "/cases/c/originals/fotos/foto.jpg")
	if err != nil || !strings.HasPrefix(d.Description, descPrefix+`{"role":"original","original":{"path":"fotos/foto.jpg"`) {
		t.Errorf("original doc = %+v, %v", d, err)
	}
	m, _ := casefile.Load(root)
	att := ""
	for _, x := range m.Derived {
		if strings.HasSuffix(x.Path, "plano.pdf") {
			att = x.Path
		}
	}
	if d, err := oa.Get(ctx, "/cases/c/"+att); err != nil || !strings.Contains(d.Description, `"attachment":"plano.pdf"`) || !strings.Contains(d.Description, `"from":"Ana <ana@example.com>"`) {
		t.Errorf("attachment doc = %+v, %v", d, err)
	}

	// Nothing changed: nothing is uploaded.
	res, err = Sync(ctx, opts)
	if err != nil || len(res.Uploaded) != 0 || res.Unchanged != 5 || oa.uploads != 5 {
		t.Fatalf("second sync = %+v, %v", res, err)
	}

	// Restore into an empty folder gives the same files and manifest.
	dst := t.TempDir()
	rr, err := Restore(ctx, RestoreOptions{Root: dst, Remote: "/cases/c", Client: oa})
	if err != nil || rr.Downloaded != 5 || len(rr.Conflicts) != 0 {
		t.Fatalf("restore = %+v, %v", rr, err)
	}
	for _, p := range []string{"aviso.eml", "fotos/foto.jpg", "references/l.txt", att} {
		a, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(p)))
		if err != nil || string(a) != string(b) {
			t.Errorf("%s differs after restore: %v", p, err)
		}
	}
	m2, _ := casefile.Load(dst)
	if len(m2.Originals) != 3 || len(m2.Derived) != 2 {
		t.Errorf("restored manifest: %d originals, %d derived", len(m2.Originals), len(m2.Derived))
	}
	// A restored folder syncs nothing.
	res, err = Sync(ctx, SyncOptions{Root: dst, Remote: "/cases/c", Client: oa})
	if err != nil || len(res.Uploaded) != 0 {
		t.Errorf("sync after restore = %+v, %v", res, err)
	}

	// A local original that differs is kept, and reported.
	if err := os.WriteFile(filepath.Join(dst, "fotos/foto.jpg"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr, err = Restore(ctx, RestoreOptions{Root: dst, Remote: "/cases/c", Client: oa})
	if err != nil || len(rr.Conflicts) != 1 || rr.Conflicts[0] != "fotos/foto.jpg" || rr.Kept != 4 {
		t.Fatalf("conflict restore = %+v, %v", rr, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "fotos/foto.jpg")); string(b) != "changed" {
		t.Error("local original overwritten")
	}
}

func TestRestoreSkipsForeignAndUnsafe(t *testing.T) {
	oa := newFakeOA()
	ctx := context.Background()
	if _, err := oa.CreateFolder(ctx, "/", "cases"); err != nil {
		t.Fatal(err)
	}
	if _, err := oa.CreateFolder(ctx, "/cases", "c"); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("x"))
	sha := hex.EncodeToString(sum[:])
	for name, desc := range map[string]string{
		"hand.txt": "a document made by hand",
		"evil":     descPrefix + `{"role":"original","original":{"path":"../evil","sha256":"` + sha + `"}}`,
		"abs":      descPrefix + `{"role":"original","original":{"path":"/etc/evil","sha256":"` + sha + `"}}`,
	} {
		if _, err := oa.Upload(ctx, tmp, "/cases/c", name, desc); err != nil {
			t.Fatal(err)
		}
	}
	dst := t.TempDir()
	rr, err := Restore(ctx, RestoreOptions{Root: dst, Remote: "/cases/c", Client: oa})
	if err != nil || rr.Skipped != 3 || rr.Downloaded != 0 {
		t.Errorf("restore = %+v, %v", rr, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "evil")); err == nil {
		t.Error("file written outside the case folder")
	}
}

func TestDefaultRemote(t *testing.T) {
	if got := DefaultRemote("/tmp/Caso Garaje 2026"); got != "/cases/caso-garaje-2026" {
		t.Errorf("DefaultRemote = %q", got)
	}
}

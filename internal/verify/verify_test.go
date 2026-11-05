package verify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
)

func TestRun(t *testing.T) {
	root := t.TempDir()
	eml, err := os.ReadFile("../email/testdata/nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "a.eml"), eml)
	mustWrite(t, filepath.Join(root, "b.zip"), []byte("PK not a zip"))
	mustWrite(t, filepath.Join(root, "c.pdf"), []byte("%PDF"))
	// Ingest fails on a broken zip, so ingest it as b.bin and rename it after.
	if err := os.Rename(filepath.Join(root, "b.zip"), filepath.Join(root, "b.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}

	res, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || res.Originals != 3 || res.Derived == 0 || len(res.New) != 0 {
		t.Fatalf("clean case = %+v", res)
	}

	m, _ := casefile.Load(root)
	derived := m.Derived[0].Path
	mustWrite(t, filepath.Join(root, "c.pdf"), []byte("%PDF changed"))
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(derived))); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "new.txt"), []byte("new"))
	// A zip original that no longer reads.
	if err := os.Rename(filepath.Join(root, "b.bin"), filepath.Join(root, "b.zip")); err != nil {
		t.Fatal(err)
	}
	for i := range m.Originals {
		if m.Originals[i].Path == "b.bin" {
			m.Originals[i].Path, m.Originals[i].Kind = "b.zip", casefile.KindZip
		}
	}
	if err := casefile.Save(root, m); err != nil {
		t.Fatal(err)
	}

	res, err = Run(root)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, p := range res.Problems {
		kinds[p.Path] = p.Kind
	}
	if kinds["c.pdf"] != ProblemChanged || kinds[derived] != ProblemMissing || kinds["b.zip"] != ProblemZip || len(res.Problems) != 3 {
		t.Errorf("problems = %+v", res.Problems)
	}
	if len(res.New) != 1 || res.New[0] != "new.txt" {
		t.Errorf("new = %v", res.New)
	}
}

func mustWrite(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

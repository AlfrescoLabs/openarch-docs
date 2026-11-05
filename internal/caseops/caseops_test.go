package caseops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
)

var now = func() time.Time { return time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC) }

func TestInit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "caso")
	res, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"references/", "pasted/", "notes/", "notes/drafts/", "AGENTS.md", ".claude/skills/openarch-docs/SKILL.md"} {
		if !strings.Contains(strings.Join(res.Created, ","), want) {
			t.Errorf("not created: %s (%v)", want, res.Created)
		}
	}
	// A second run changes nothing, and keeps a guide the user wrote.
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Init(root)
	if err != nil || len(res.Created) != 0 || len(res.Kept) != 2 {
		t.Errorf("second init = %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")); string(b) != "mine" {
		t.Error("AGENTS.md overwritten")
	}
}

func TestAdd(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	src := filepath.Join(outside, "ley 49-1960.pdf")
	if err := os.WriteFile(src, []byte("%PDF ley"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Add(AddOptions{Root: root, Files: []string{src}, Dir: casefile.ReferencesDir, SourceURL: "https://example.org/ley", Now: now})
	if err != nil || len(res.Files) != 1 || res.Files[0].Path != "references/ley 49-1960.pdf" || res.Files[0].Status != StatusAdded {
		t.Fatalf("add = %+v, %v", res, err)
	}
	res, err = Add(AddOptions{Root: root, Text: "De: Ana\nAsunto: Ultima actualizacion\n\nTexto pegado.", Name: "ultima", Note: "pegado de Gmail", Now: now})
	if err != nil || res.Files[0].Path != "pasted/ultima.txt" {
		t.Fatalf("add text = %+v, %v", res, err)
	}
	m, _ := casefile.Load(root)
	ref, _ := m.Original("references/ley 49-1960.pdf")
	if ref.Role != casefile.RoleReference || ref.Added == nil || ref.Added.How != "copied" || ref.Added.From != src || ref.Added.SourceURL != "https://example.org/ley" {
		t.Errorf("reference = %+v %+v", ref, ref.Added)
	}
	pasted, _ := m.Original("pasted/ultima.txt")
	if pasted.Added == nil || pasted.Added.How != "pasted" || pasted.Added.Note != "pegado de Gmail" || pasted.Kind != casefile.KindText {
		t.Errorf("pasted = %+v", pasted)
	}

	// The same file again is fine; other content with the same name is not.
	if res, err := Add(AddOptions{Root: root, Files: []string{src}, Dir: casefile.ReferencesDir, Now: now}); err != nil || res.Files[0].Status != StatusPresent {
		t.Errorf("same file = %+v, %v", res, err)
	}
	if err := os.WriteFile(src, []byte("%PDF otra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(AddOptions{Root: root, Files: []string{src}, Dir: casefile.ReferencesDir, Now: now}); err == nil || !strings.Contains(err.Error(), "never replaced") {
		t.Errorf("other content = %v", err)
	}
	// A file in the case folder, a plugin folder or a hidden folder is refused.
	if _, err := Add(AddOptions{Root: root, Files: []string{filepath.Join(root, "pasted/ultima.txt")}, Now: now}); err == nil {
		t.Error("file in the case folder accepted")
	}
	for _, d := range []string{"extracted", "notes/x", ".secret", "../out"} {
		if _, err := Add(AddOptions{Root: root, Text: "x", Dir: d, Now: now}); err == nil {
			t.Errorf("dir %q accepted", d)
		}
	}

	// The provenance stays after a reparse.
	m, _ = casefile.Load(root)
	for i := range m.Originals {
		m.Originals[i].Parser = 0
		m.Originals[i].Kind = casefile.KindEmail
	}
	if err := casefile.Save(root, m); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	m, _ = casefile.Load(root)
	if p, _ := m.Original("pasted/ultima.txt"); p.Added == nil || p.Added.How != "pasted" {
		t.Errorf("provenance lost after reparse: %+v", p)
	}
}

func TestStatus(t *testing.T) {
	root := t.TempDir()
	eml, err := os.ReadFile("../email/testdata/nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.eml"), eml, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nuevo.txt"), []byte("nuevo"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := GetStatus(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	pending := map[string]int{}
	for _, s := range st.Steps {
		pending[s.Command] = s.Pending
	}
	// The PDF attachment and the PNG inline image wait for extract.
	if st.Next != "ingest" || pending["ingest"] != 1 || pending["extract"] != 2 || pending["index"] != 1 || pending["sync"] == 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestCollect(t *testing.T) {
	root := t.TempDir()
	eml, _ := os.ReadFile("../email/testdata/nested.eml")
	if err := os.WriteFile(filepath.Join(root, "a.eml"), eml, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "justificante.pdf"), []byte("%PDF j"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	m, _ := casefile.Load(root)
	att := ""
	for _, d := range m.Derived {
		if d.Attachment == "Condiciones técnicas.pdf" {
			att = d.Path
		}
	}
	res, err := Collect(CollectOptions{Root: root, Refs: []string{"justificante.pdf", att}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.Out != "notes/outbox/2026-11-05" || len(res.Files) != 2 || res.Files[1].File != "Condiciones técnicas.pdf" || res.Files[1].Email != "Contestación expediente 9999" {
		t.Fatalf("collect = %+v", res)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes/outbox/2026-11-05/collect.json"))
	var back CollectResult
	if err != nil || json.Unmarshal(data, &back) != nil || len(back.Files) != 2 {
		t.Errorf("collect.json = %s, %v", data, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "notes/outbox/2026-11-05/Condiciones técnicas.pdf")); string(b) != "%PDF-1.4 fake" {
		t.Errorf("copy = %q", b)
	}
	// The copies are not originals.
	paths, _ := casefile.FindOriginals(root)
	if len(paths) != 2 {
		t.Errorf("originals = %v", paths)
	}
	for _, bad := range [][]string{{"../x"}, {"nosuch.pdf"}} {
		if _, err := Collect(CollectOptions{Root: root, Refs: bad, Now: now}); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := Collect(CollectOptions{Root: root, Refs: []string{"justificante.pdf"}, Out: "references", Now: now}); err == nil {
		t.Error("collect into references/ accepted")
	}
}

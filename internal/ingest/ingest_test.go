package ingest

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
)

var now = func() time.Time { return time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC) }

// newCase writes a synthetic case folder and returns its path.
func newCase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	eml, err := os.ReadFile("../email/testdata/nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "Fwd Condiciones nº 9999.eml", eml)
	write(t, root, "WhatsApp Chat - Instalador.zip", mkzip(t, map[string]string{
		"_chat.txt":                              "[10/9/26, 18:10:04] Instalador: <attached: 00000023-PHOTO-2026-09-10-18-10-04.jpg>\n[13/9/26, 9:00:00] Cliente: gracias\n",
		"00000023-PHOTO-2026-09-10-18-10-04.jpg": "jpeg",
	}))
	write(t, root, "fotos.zip", mkzip(t, map[string]string{"../../evil.txt": "x", "sub/a.png": "png"}))
	write(t, root, "escritura.pdf", []byte("%PDF-1.4"))
	write(t, root, "references/ley.pdf", []byte("%PDF-1.4 ley"))
	write(t, root, "AGENTS.md", []byte("rules"))
	write(t, root, ".DS_Store", []byte("x"))
	write(t, root, "notes/estado.md", []byte("nota"))
	write(t, root, "extracted/email-01/body.txt", []byte("made by hand"))
	return root
}

func write(t *testing.T, root, name string, data []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkzip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func derived(m *casefile.Manifest, suffix string) (casefile.Derived, bool) {
	i := slices.IndexFunc(m.Derived, func(d casefile.Derived) bool { return strings.HasSuffix(d.Path, suffix) })
	if i < 0 {
		return casefile.Derived{}, false
	}
	return m.Derived[i], true
}

func TestRun(t *testing.T) {
	root := newCase(t)
	res, err := Run(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 5 || res.Changed != 0 {
		t.Fatalf("result = %+v", res)
	}
	m, err := casefile.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, o := range m.Originals {
		paths = append(paths, o.Path+":"+o.Kind+":"+o.Role)
	}
	want := []string{
		"Fwd Condiciones nº 9999.eml:email:case", "WhatsApp Chat - Instalador.zip:zip:case", "escritura.pdf:pdf:case",
		"fotos.zip:zip:case", "references/ley.pdf:pdf:reference",
	}
	if !slices.Equal(paths, want) {
		t.Errorf("originals = %q", paths)
	}

	// Email: the body, the inline image, the nested message and its attachment.
	i := slices.IndexFunc(m.Derived, func(d casefile.Derived) bool {
		return strings.Count(d.Path, "/") == 2 && strings.HasSuffix(d.Path, "/message.txt")
	})
	if i < 0 {
		t.Fatal("no top message.txt")
	}
	body := m.Derived[i]
	if !strings.HasPrefix(body.Path, "extracted/fwd-condiciones-n-9999-") {
		t.Fatalf("body = %+v", body)
	}
	if body.Message.From != "Ana Lopez <ana@example.com>" || len(body.Forwarded) != 1 || body.Forwarded[0].From != "Instalador Ejemplo <instalador@example.net>" {
		t.Errorf("body provenance = %+v", body)
	}
	text, _ := os.ReadFile(filepath.Join(root, body.Path))
	if !strings.HasPrefix(string(text), "From: Ana Lopez <ana@example.com>\n") || !strings.Contains(string(text), "Te reenvío") {
		t.Errorf("message.txt = %q", text)
	}
	if d, ok := derived(m, "/inline/part-1.2.png"); !ok || d.Kind != KindInline || d.Part != "1.2" {
		t.Errorf("inline = %+v", d)
	}
	if d, ok := derived(m, "/message-1/message.eml"); !ok || d.Kind != KindMessage || d.Message.From != "Distribuidora Ejemplo <avisos@example.com>" || len(d.Chain) != 1 {
		t.Errorf("nested = %+v", d)
	}
	att, ok := derived(m, "/message-1/attachments/Condiciones técnicas.pdf")
	if !ok || att.Attachment != "Condiciones técnicas.pdf" || att.Message.Subject != "Contestación expediente 9999" ||
		len(att.Chain) != 1 || att.Chain[0].From != "Ana Lopez <ana@example.com>" || att.Source != "Fwd Condiciones nº 9999.eml" {
		t.Errorf("attachment = %+v", att)
	}

	// Chat: who sent the photo and when.
	media, ok := derived(m, "/media/00000023-PHOTO-2026-09-10-18-10-04.jpg")
	if !ok || media.Chat == nil || media.Chat.Sender != "Instalador" || media.Chat.Time != "2026-09-10T18:10:04" {
		t.Errorf("media = %+v", media)
	}
	if _, ok := derived(m, "/messages.json"); !ok {
		t.Error("no messages.json")
	}

	// Other zip: files stay inside the target folder.
	if _, ok := derived(m, "/files/evil.txt"); !ok {
		t.Error("zip entry with .. not kept inside the folder")
	}
	if _, err := os.Stat(filepath.Join(root, "..", "evil.txt")); err == nil {
		t.Error("zip entry written outside the case folder")
	}
	if _, ok := derived(m, "/files/sub/a.png"); !ok {
		t.Error("zip subfolder not kept")
	}

	// Files made by hand in extracted/ are left alone.
	if b, _ := os.ReadFile(filepath.Join(root, "extracted/email-01/body.txt")); string(b) != "made by hand" {
		t.Error("hand-made file changed")
	}

	// A second run changes nothing.
	again, err := Run(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if again.Added != 0 || again.Changed != 0 || again.Derived != 0 {
		t.Errorf("second run = %+v", again)
	}
}

func TestRunChanged(t *testing.T) {
	root := newCase(t)
	if _, err := Run(Options{Root: root, Now: now}); err != nil {
		t.Fatal(err)
	}
	m, _ := casefile.Load(root)
	old, _ := derived(m, "/media/00000023-PHOTO-2026-09-10-18-10-04.jpg")

	write(t, root, "WhatsApp Chat - Instalador.zip", mkzip(t, map[string]string{"_chat.txt": "[13/9/26, 9:00:00] Cliente: nuevo\n"}))
	res, err := Run(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed != 1 || res.Added != 0 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, old.Path)); !os.IsNotExist(err) {
		t.Error("old derived file not removed")
	}
	m, _ = casefile.Load(root)
	if len(m.Originals) != 5 {
		t.Errorf("originals = %d", len(m.Originals))
	}
	if _, ok := derived(m, "/media/00000023-PHOTO-2026-09-10-18-10-04.jpg"); ok {
		t.Error("old derived entry kept")
	}
}

func TestRunDryRun(t *testing.T) {
	root := newCase(t)
	res, err := Run(Options{Root: root, DryRun: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 5 {
		t.Errorf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, casefile.ManifestFile)); !os.IsNotExist(err) {
		t.Error("dry run wrote the manifest")
	}
}

func TestRunReparse(t *testing.T) {
	root := newCase(t)
	if _, err := Run(Options{Root: root, Now: now}); err != nil {
		t.Fatal(err)
	}
	m, _ := casefile.Load(root)
	att, _ := derived(m, "/message-1/attachments/Condiciones técnicas.pdf")
	// An extract result of an attachment, and one of a file the parser no longer writes.
	page := casefile.Derived{Source: att.Source, From: att.Path, Kind: "page-text", Page: 1}
	if _, err := m.WriteDerived(root, att.Path+".extract/page-001.txt", []byte("texto"), page); err != nil {
		t.Fatal(err)
	}
	gone := casefile.Derived{Source: att.Source, From: att.Path + "-old.pdf", Kind: "page-text", Page: 1}
	goneP, err := m.WriteDerived(root, att.Path+"-old.pdf.extract/page-001.txt", []byte("viejo"), gone)
	if err != nil {
		t.Fatal(err)
	}
	// Mark every original as parsed by version 1.
	for i := range m.Originals {
		m.Originals[i].Parser = 1
	}
	if err := casefile.Save(root, m); err != nil {
		t.Fatal(err)
	}

	res, err := Run(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	// The email and the two zips are parsed again; the PDFs are not.
	if res.Reparsed != 3 || res.Added != 0 || res.Changed != 0 {
		t.Fatalf("result = %+v", res)
	}
	m, _ = casefile.Load(root)
	if _, ok := derived(m, "Condiciones técnicas.pdf.extract/page-001.txt"); !ok {
		t.Error("extract result of a file that still exists was dropped")
	}
	if _, ok := derived(m, "-old.pdf.extract/page-001.txt"); ok {
		t.Error("extract result of a file that is gone was kept")
	}
	if _, err := os.Stat(filepath.Join(root, goneP)); !os.IsNotExist(err) {
		t.Error("file of the dropped result still exists")
	}
	for _, o := range m.Originals {
		if (o.Kind == casefile.KindEmail || o.Kind == casefile.KindZip) && o.Parser != ParserVersion {
			t.Errorf("%s parser = %d", o.Path, o.Parser)
		}
	}
	// Nothing to do the next time.
	again, err := Run(Options{Root: root, Now: now})
	if err != nil || again.Reparsed != 0 {
		t.Errorf("again = %+v, %v", again, err)
	}
}

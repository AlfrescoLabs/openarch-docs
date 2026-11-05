package extract

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/tools"
)

// fakeRunner writes the files that the real programs would write. Page 1 has
// text; page 2 is a scan with a photo.
type fakeRunner struct {
	calls []string
	fail  string
}

func (f *fakeRunner) Run(_ context.Context, input, outDir string, cmds []tools.Cmd) error {
	f.calls = append(f.calls, filepath.Base(input))
	if f.fail != "" && strings.Contains(input, f.fail) {
		return errors.New("broken file")
	}
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(data), 0o644); err != nil {
			panic(err)
		}
	}
	for _, c := range cmds {
		switch c.Args[0] {
		case "pdfinfo":
			write(c.Stdout, "Title: x\nPages:          2\n")
		case "pdftotext":
			write("text.txt", "Condiciones de suministro, expediente 9999, centralizacion en el sotano -2\f\f")
		case "pdfimages":
			write(c.Stdout, "page   num  type   width height color\n------------\n   1     0 image      40    40  rgb\n   2     1 image    1700  2200  rgb\n   2     2 smask    1700  2200  gray\n")
		case "pdftoppm":
			write(filepath.Base(c.Args[len(c.Args)-1])+".png", "png")
		case "tesseract":
			write(filepath.Base(c.Args[2])+".txt", "texto OCR")
		}
	}
	return nil
}

func newCase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"escritura.pdf": "%PDF", "foto.heic": "heic",
		"mail.eml": "From: a@example.com\nSubject: x\nContent-Type: multipart/mixed; boundary=b\n\n--b\nContent-Type: text/plain\n\nhola\n--b\nContent-Type: application/pdf\nContent-Disposition: attachment; filename=plano.pdf\n\n%PDF\n--b\nContent-Type: image/png\nContent-Disposition: inline\n\npng\n--b--\n",
	}
	for n, data := range files {
		if err := os.WriteFile(filepath.Join(root, n), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	return root
}

func options(root string, r tools.Runner) Options {
	langs, _ := lang.Get([]string{"es", "en"})
	return Options{Root: root, Runner: r, Languages: langs, DPI: 150, MinText: 50}
}

func TestRun(t *testing.T) {
	root := newCase(t)
	r := &fakeRunner{}
	res, err := Run(context.Background(), options(root, r))
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 3 || res.Unsupported != 1 || res.Failed != 0 || !slices.Equal(res.Languages, []string{"es", "en"}) {
		t.Fatalf("result = %+v", res)
	}
	byPath := map[string]File{}
	for _, f := range res.Files {
		byPath[f.Path] = f
	}
	pdf := byPath["escritura.pdf"]
	if pdf.Pages != 2 || !slices.Equal(pdf.OCRPages, []int{2}) || !slices.Equal(pdf.ImagePages, []int{2}) {
		t.Errorf("pdf = %+v", pdf)
	}
	if byPath["foto.heic"].Status != StatusUnsupported {
		t.Errorf("heic = %+v", byPath["foto.heic"])
	}

	m, _ := casefile.Load(root)
	find := func(suffix string) casefile.Derived {
		i := slices.IndexFunc(m.Derived, func(d casefile.Derived) bool { return strings.HasSuffix(d.Path, suffix) })
		if i < 0 {
			t.Fatalf("no derived file %s", suffix)
		}
		return m.Derived[i]
	}
	p1 := find("escritura-" + m.Originals[0].SHA256[:8] + "/page-001.txt")
	if p1.Method != MethodTextLayer || p1.Page != 1 || p1.From != "escritura.pdf" || p1.Source != "escritura.pdf" || p1.Images != 0 {
		t.Errorf("page 1 text = %+v", p1)
	}
	ocr := find("/page-002.ocr.txt")
	if ocr.Method != MethodOCR || ocr.Images != 1 || !slices.Equal(ocr.Languages, []string{"es", "en"}) {
		t.Errorf("page 2 ocr = %+v", ocr)
	}
	find("/page-002.png")
	if slices.ContainsFunc(m.Derived, func(d casefile.Derived) bool { return strings.HasSuffix(d.Path, "/page-002.txt") }) {
		t.Error("empty text layer written")
	}
	// An attachment of an email: results next to it, linked to the email.
	att := find("/attachments/plano.pdf.extract/page-001.txt")
	if att.Source != "mail.eml" || !strings.HasSuffix(att.From, "/attachments/plano.pdf") {
		t.Errorf("attachment page = %+v", att)
	}
	if find("/inline/part-3.png.extract/ocr.txt").Kind != KindImageOCR {
		t.Error("inline image has no OCR")
	}
	// No work folders are left.
	if left, _ := filepath.Glob(filepath.Join(root, "extracted", ".work-*")); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}

	// A second run skips everything; --force runs again without duplicates.
	r.calls = nil
	res, err = Run(context.Background(), options(root, r))
	if err != nil || res.Skipped != 3 || len(r.calls) != 0 {
		t.Fatalf("second run = %+v, %v, calls %v", res, err, r.calls)
	}
	before := len(m.Derived)
	opts := options(root, r)
	opts.Force = true
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	m, _ = casefile.Load(root)
	if len(m.Derived) != before {
		t.Errorf("force: %d derived files, want %d", len(m.Derived), before)
	}
}

func TestRunFailure(t *testing.T) {
	root := newCase(t)
	res, err := Run(context.Background(), options(root, &fakeRunner{fail: "escritura"}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 || res.Done != 2 {
		t.Errorf("result = %+v", res)
	}
}

func TestParsers(t *testing.T) {
	if n := pageCount("Producer: x\nPages:   12\n"); n != 12 {
		t.Errorf("pageCount = %d", n)
	}
	if got := splitPages("a\fb\f", 3); !slices.Equal(got, []string{"a", "b", ""}) {
		t.Errorf("splitPages = %q", got)
	}
	if n := letters("  1, a. ñ!"); n != 3 {
		t.Errorf("letters = %d", n)
	}
}

// TestRealTools runs poppler and tesseract on the fixture PDFs. Set
// OPENARCH_DOCS_TOOLS_TEST to docker or local to run it.
func TestRealTools(t *testing.T) {
	mode := os.Getenv("OPENARCH_DOCS_TOOLS_TEST")
	var runner tools.Runner
	switch mode {
	case "docker":
		d := tools.Docker{}
		if err := d.EnsureImage(context.Background()); err != nil {
			t.Fatal(err)
		}
		runner = d
	case "local":
		runner = tools.Local{}
	default:
		t.Skip("set OPENARCH_DOCS_TOOLS_TEST=docker or local")
	}
	root := t.TempDir()
	for _, n := range []string{"condiciones.pdf", "escritura-scan.pdf"} {
		data, err := os.ReadFile("testdata/" + n)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	langs, _ := lang.Get(lang.Codes())
	if mode == "local" {
		langs, _ = lang.Get([]string{"en"})
	}
	res, err := Run(context.Background(), Options{Root: root, Runner: runner, Languages: langs, DPI: 100, MinText: 50})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 2 || res.Failed != 0 {
		t.Fatalf("result = %+v", res)
	}
	m, _ := casefile.Load(root)
	texts := map[string]string{}
	for _, d := range m.Derived {
		if d.Kind == KindPageText || d.Kind == KindPageOCR {
			b, _ := os.ReadFile(filepath.Join(root, d.Path))
			texts[d.From+":"+d.Method] = string(b)
		}
	}
	if !strings.Contains(texts["condiciones.pdf:text-layer"], "Calle Falsa 12") {
		t.Errorf("text layer = %q", texts["condiciones.pdf:text-layer"])
	}
	if !strings.Contains(texts["escritura-scan.pdf:ocr"], "Calle Falsa 12") {
		t.Errorf("ocr = %q", texts["escritura-scan.pdf:ocr"])
	}
}

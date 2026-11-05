package vision

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/extract"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

type fakeChat struct {
	calls    int
	messages []models.Message
}

func (f *fakeChat) Chat(_ context.Context, _ string, messages []models.Message, _ models.ChatOptions) (string, error) {
	f.calls++
	f.messages = messages
	return "Okay, here is the description:\n\n**Type:** photo\nText: none\nContent: un cuarto\nLocation: none\n\nLet me know if you need more.", nil
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newCase(t *testing.T) (string, *casefile.Manifest) {
	t.Helper()
	root := t.TempDir()
	write := func(name string, data []byte) {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("foto.png", pngBytes(t, 400, 300))
	write("logo.png", pngBytes(t, 80, 40))
	write("foto.heic", []byte("heic"))
	write("plano.pdf", []byte("%PDF"))
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	// Two page images as extract writes them: page 1 with a plan, page 2 text only.
	m, _ := casefile.Load(root)
	dir := casefile.DerivedDir("plano.pdf", m.Originals[slices.IndexFunc(m.Originals, func(o casefile.Original) bool { return o.Path == "plano.pdf" })].SHA256)
	for p, images := range map[int]int{1: 1, 2: 0} {
		d := casefile.Derived{Source: "plano.pdf", From: "plano.pdf", Page: p, Images: images, Kind: extract.KindPageImage, MediaType: "image/png"}
		if _, err := m.WriteDerived(root, filepath.ToSlash(filepath.Join(dir, "page-00"+string(rune('0'+p))+".png")), pngBytes(t, 300, 400), d); err != nil {
			t.Fatal(err)
		}
	}
	if err := casefile.Save(root, m); err != nil {
		t.Fatal(err)
	}
	return root, m
}

func options(root string, c Chatter) Options {
	es, _ := lang.Get([]string{"es"})
	return Options{Root: root, Chat: c, Model: "vision-model", Language: es[0]}
}

func TestDescribe(t *testing.T) {
	root, _ := newCase(t)
	chat := &fakeChat{}
	res, err := Describe(context.Background(), options(root, chat))
	if err != nil {
		t.Fatal(err)
	}
	// foto.png and page 1; logo.png is small, foto.heic unsupported, page 2 has no images.
	if res.Done != 2 || res.Small != 1 || res.Unsupported != 1 || res.Failed != 0 || chat.calls != 2 {
		t.Fatalf("result = %+v, calls %d", res, chat.calls)
	}
	parts := chat.messages[0].Content.([]models.Part)
	if len(parts) != 3 || parts[1].Type != "image_url" || !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") ||
		!strings.Contains(parts[2].Text, "Spanish") {
		t.Errorf("parts = %+v", parts)
	}
	m, _ := casefile.Load(root)
	var page, photo casefile.Derived
	for _, d := range m.Derived {
		if d.Kind != KindDescription {
			continue
		}
		if d.Page == 1 {
			page = d
		} else {
			photo = d
		}
	}
	if page.From != "plano.pdf" || !strings.HasSuffix(page.Path, "/page-001.describe.txt") || page.Model != "vision-model" || page.Method != MethodVision {
		t.Errorf("page description = %+v", page)
	}
	if photo.From != "foto.png" || !strings.HasSuffix(photo.Path, "/describe.txt") || !slices.Equal(photo.Languages, []string{"es"}) {
		t.Errorf("photo description = %+v", photo)
	}
	text, _ := os.ReadFile(filepath.Join(root, photo.Path))
	if string(text) != "**Type:** photo\nText: none\nContent: un cuarto\nLocation: none\n" {
		t.Errorf("description = %q", text)
	}

	// Second run: nothing to do. --all-pages adds page 2.
	chat.calls = 0
	res, err = Describe(context.Background(), options(root, chat))
	if err != nil || res.Skipped != 2 || chat.calls != 0 {
		t.Fatalf("second run = %+v, %v", res, err)
	}
	opts := options(root, chat)
	opts.AllPages = true
	res, err = Describe(context.Background(), opts)
	if err != nil || res.Done != 1 {
		t.Fatalf("all pages = %+v, %v", res, err)
	}
	// --force replaces the descriptions, without duplicates.
	opts.Force = true
	if _, err := Describe(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	m, _ = casefile.Load(root)
	n := 0
	for _, d := range m.Derived {
		if d.Kind == KindDescription {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d descriptions after force, want 3", n)
	}
}

func TestDescribeMax(t *testing.T) {
	root, _ := newCase(t)
	opts := options(root, &fakeChat{})
	opts.Max = 1
	res, err := Describe(context.Background(), opts)
	if err != nil || res.Done != 1 || res.Pending != 1 {
		t.Errorf("max = %+v, %v", res, err)
	}
}

func TestPrepare(t *testing.T) {
	mt, out, w, h, err := Prepare(pngBytes(t, 3200, 1600))
	if err != nil || mt != "image/jpeg" || w != 3200 || h != 1600 {
		t.Fatalf("Prepare = %s, %d x %d, %v", mt, w, h, err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != maxSide || cfg.Height != maxSide/2 {
		t.Errorf("scaled = %+v, %v", cfg, err)
	}
	small := pngBytes(t, 100, 100)
	if mt, out, _, _, _ := Prepare(small); mt != "image/png" || !bytes.Equal(out, small) {
		t.Error("small image changed")
	}
	if _, _, _, _, err := Prepare([]byte("heic")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v", err)
	}
}

func TestLook(t *testing.T) {
	root, _ := newCase(t)
	chat := &fakeChat{}
	res, err := Look(context.Background(), root, chat, "v", "¿Es el mismo cuarto?", []string{"foto.png", "plano.pdf#1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 2 || res.Images[1].Label != "plano.pdf p.1" || !strings.HasSuffix(res.Images[1].Path, "/page-001.png") {
		t.Errorf("images = %+v", res.Images)
	}
	parts := chat.messages[0].Content.([]models.Part)
	if len(parts) != 5 || parts[0].Text != "Image 1: foto.png" || !strings.Contains(parts[4].Text, "Answer in Spanish.") {
		t.Errorf("parts = %+v", parts)
	}
	for _, bad := range []string{"../etc/passwd", "plano.pdf#9", "plano.pdf#x", "nosuch.png"} {
		if _, err := Look(context.Background(), root, chat, "v", "q", []string{bad}); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	in := "Sure! Here it is.\n**Type:** plan\nContent: x\n\nI hope this helps.\nLet me know if you want more detail."
	if got := cleanDescription(in); got != "**Type:** plan\nContent: x" {
		t.Errorf("cleanDescription = %q", got)
	}
}

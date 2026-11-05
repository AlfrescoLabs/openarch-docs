package index

import (
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

// fakeEmbedder makes bag-of-words vectors: texts that share words are close.
// Inputs longer than maxLen fail as too large, as on Docker Model Runner.
type fakeEmbedder struct{ calls, inputs, maxLen int }

func (f *fakeEmbedder) Embed(_ context.Context, model string, inputs []string) ([][]float32, error) {
	f.calls++
	f.inputs += len(inputs)
	for _, in := range inputs {
		if f.maxLen > 0 && len([]rune(in)) > f.maxLen {
			return nil, &models.ModelError{Model: model, Status: 500, Body: `{"error":{"message":"input (548 tokens) is too large to process"}}`}
		}
	}
	out := make([][]float32, len(inputs))
	for i, in := range inputs {
		v := make([]float32, 64)
		for _, w := range strings.FieldsFunc(strings.ToLower(in), func(r rune) bool { return !unicode.IsLetter(r) }) {
			if len(w) < 4 {
				continue
			}
			h := fnv.New32a()
			_, _ = h.Write([]byte(w))
			v[h.Sum32()%64]++
		}
		out[i] = models.Normalize(v)
	}
	return out, nil
}

func newCase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	eml, err := os.ReadFile("../email/testdata/nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"reenvio.eml":         string(eml),
		"references/ley.txt":  "Articulo 17. La instalacion de un punto de recarga en el aparcamiento solo requiere comunicacion previa a la comunidad.",
		"notes/estado.md":     "# Estado\n\nPendiente: comparar el plano con las fotografias del cuarto de contadores.",
		"notes/.hidden.md":    "hidden note with enough letters to be indexed if read",
		"notes/foto.png":      "not a note",
		"apuntes/llamada.txt": "Llamada telefonica con la distribuidora: confirman el expediente y la fecha de instalacion.",
	}
	for n, data := range files {
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

func open(t *testing.T, root string) *Index {
	t.Helper()
	x, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return x
}

func TestBuildAndSearch(t *testing.T) {
	root := newCase(t)
	x := open(t, root)
	emb := &fakeEmbedder{}
	ctx := context.Background()
	res, err := x.Build(ctx, BuildOptions{Root: root, Embedder: emb, Model: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	// Two email bodies, the reference, the text original and one note.
	if res.Files != 5 || res.Updated != 5 || res.Chunks != 5 || res.Embedded != 5 || res.Pending != 0 {
		t.Fatalf("build = %+v", res)
	}

	hits, err := x.Search(ctx, SearchOptions{Query: "sotano centralizacion", Embedder: emb, Model: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Text, "sótano") || hits[0].Role != casefile.RoleCase {
		t.Fatalf("hits = %+v", hits)
	}
	h := hits[0]
	if h.Cite != `reenvio.eml, email "Contestación expediente 9999" of 2026-10-29T12:00:00+01:00` || h.Sender != "Distribuidora Ejemplo <avisos@example.com>" ||
		!strings.HasSuffix(h.Path, "/message-1/message.txt") || h.Source != "reenvio.eml" {
		t.Errorf("first hit = %+v", h)
	}

	hits, err = x.Search(ctx, SearchOptions{Query: "punto de recarga", Roles: []string{casefile.RoleReference}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Cite != "ley.txt" || hits[0].Matched[0] != "text" {
		t.Errorf("reference hits = %+v", hits)
	}
	hits, err = x.Search(ctx, SearchOptions{Query: "fotografias contadores", Roles: []string{RoleNote}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Cite != "note notes/estado.md" {
		t.Errorf("note hits = %+v", hits)
	}
	// Meaning alone finds a chunk that shares no exact query word with FTS.
	hits, err = x.Search(ctx, SearchOptions{Query: "!!", Embedder: emb, Model: "m1", Limit: 2})
	if err != nil || len(hits) != 2 || hits[0].Matched[0] != "meaning" {
		t.Errorf("meaning only = %+v, %v", hits, err)
	}

	// A second build changes nothing and calls no model.
	emb.calls = 0
	res, err = x.Build(ctx, BuildOptions{Root: root, Embedder: emb, Model: "m1"})
	if err != nil || res.Updated != 0 || emb.calls != 0 {
		t.Fatalf("second build = %+v, %v, calls %d", res, err, emb.calls)
	}

	// A changed note is split again; a removed one leaves the index.
	if err := os.WriteFile(filepath.Join(root, "notes/estado.md"), []byte("Nueva nota: la reclamacion sigue abierta y falta la referencia."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes/otra.md"), []byte("Otra nota con texto suficiente para un fragmento."), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = x.Build(ctx, BuildOptions{Root: root, Embedder: emb, Model: "m1"})
	if err != nil || res.Updated != 2 || res.Embedded != 2 {
		t.Fatalf("after note change = %+v, %v", res, err)
	}
	if err := os.Remove(filepath.Join(root, "notes/otra.md")); err != nil {
		t.Fatal(err)
	}
	res, err = x.Build(ctx, BuildOptions{Root: root})
	if err != nil || res.Removed != 1 || res.Chunks != 5 {
		t.Fatalf("after note removal = %+v, %v", res, err)
	}

	// Another embedding model embeds every chunk again.
	emb.inputs = 0
	res, err = x.Build(ctx, BuildOptions{Root: root, Embedder: emb, Model: "m2"})
	if err != nil || res.Embedded != 5 || emb.inputs != 5 {
		t.Fatalf("model change = %+v, %v, inputs %d", res, err, emb.inputs)
	}
}

func TestBuildNoEmbed(t *testing.T) {
	root := newCase(t)
	x := open(t, root)
	res, err := x.Build(context.Background(), BuildOptions{Root: root})
	if err != nil || res.Pending != 5 || res.Embedded != 0 {
		t.Fatalf("build = %+v, %v", res, err)
	}
	hits, err := x.Search(context.Background(), SearchOptions{Query: "instalacion"})
	if err != nil || len(hits) == 0 {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	// The pending embeddings are made by the next run with a model.
	res, err = x.Build(context.Background(), BuildOptions{Root: root, Embedder: &fakeEmbedder{}, Model: "m1"})
	if err != nil || res.Embedded != 5 || res.Pending != 0 {
		t.Fatalf("second build = %+v, %v", res, err)
	}
}

func TestBuildTooLong(t *testing.T) {
	root := newCase(t)
	x := open(t, root)
	// Most inputs of the case are longer than 120 characters with their citation.
	res, err := x.Build(context.Background(), BuildOptions{Root: root, Embedder: &fakeEmbedder{maxLen: 120}, Model: "m1"})
	if err != nil || res.Embedded != 5 || res.Pending != 0 {
		t.Fatalf("build = %+v, %v", res, err)
	}
	// Full text search still sees the whole chunk.
	hits, err := x.Search(context.Background(), SearchOptions{Query: "instalacion", Limit: 1})
	if err != nil || len(hits) != 1 || !strings.Contains(hits[0].Text, "fecha de instalacion") {
		t.Errorf("hits = %+v, %v", hits, err)
	}
}

func TestSearchPage(t *testing.T) {
	root := t.TempDir()
	m, _ := casefile.Load(root)
	// Ten pages that all talk about the conditions; only page 6 has photos.
	for p := 1; p <= 10; p++ {
		text := "Condiciones tecnicas de suministro, condiciones generales y condiciones particulares del expediente."
		if p == 6 {
			text = "Fotografias del cuarto de contadores y de la centralizacion."
		}
		d := casefile.Derived{Source: "c.pdf", From: "c.pdf", Page: p, Kind: "page-text", Method: "text-layer"}
		if _, err := m.WriteDerived(root, "extracted/c/page-"+strconv.Itoa(p)+".txt", []byte(text), d); err != nil {
			t.Fatal(err)
		}
	}
	m.Originals = append(m.Originals, casefile.Original{Path: "c.pdf", Kind: "pdf", Role: casefile.RoleCase})
	if err := casefile.Save(root, m); err != nil {
		t.Fatal(err)
	}
	x := open(t, root)
	emb := &fakeEmbedder{}
	if _, err := x.Build(context.Background(), BuildOptions{Root: root, Embedder: emb, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	hits, err := x.Search(context.Background(), SearchOptions{Query: "las condiciones de la página 6", Embedder: emb, Model: "m", Limit: 3})
	if err != nil || len(hits) == 0 || hits[0].Page != 6 || !slices.Contains(hits[0].Matched, "page") {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	// Without a page in the query, page 6 is not first.
	hits, _ = x.Search(context.Background(), SearchOptions{Query: "las condiciones", Embedder: emb, Model: "m", Limit: 3})
	if len(hits) == 0 || hits[0].Page == 6 {
		t.Errorf("no page hits = %+v", hits)
	}
}

func TestCitation(t *testing.T) {
	byPath := map[string]casefile.Derived{
		"x/attachments/attachment-18.pdf": {Attachment: "attachment-18.pdf", Message: &casefile.MessageInfo{Subject: "Condiciones de suministro"}},
		"x/media/foto.jpg":                {Attachment: "foto.jpg", Chat: &casefile.ChatInfo{Sender: "Ana", Time: "2026-09-10T18:10:04"}},
	}
	page := casefile.Derived{Source: "a.eml", From: "x/attachments/attachment-18.pdf", Page: 6, Kind: "page-text", Method: "text-layer"}
	if got := citation(page, byPath); got != `attachment-18.pdf, attached to email "Condiciones de suministro" p.6` {
		t.Errorf("page = %q", got)
	}
	photo := casefile.Derived{Source: "chat.zip", From: "x/media/foto.jpg", Kind: "description", Method: "vision"}
	if got := citation(photo, byPath); got != "foto.jpg, sent in the chat by Ana at 2026-09-10T18:10:04 (image description)" {
		t.Errorf("photo = %q", got)
	}
}

func TestChunks(t *testing.T) {
	para := strings.Repeat("palabra ", 50) // 400 characters
	text := para + "\n\n" + para + "\n\n" + para + "\n\n" + para
	got := chunks(text)
	if len(got) < 2 {
		t.Fatalf("got %d chunks", len(got))
	}
	for i, c := range got {
		if n := len([]rune(c)); n > chunkSize {
			t.Errorf("chunk %d has %d characters", i, n)
		}
		if strings.HasPrefix(c, "alabra") || strings.HasSuffix(c, "palabr") {
			t.Errorf("chunk %d cuts a word: %q...%q", i, c[:10], c[len(c)-10:])
		}
	}
	if got := chunks("|| -- 12 ab"); len(got) != 0 {
		t.Errorf("noise kept: %q", got)
	}
	if got := chunks("Una frase corta pero con letras suficientes."); len(got) != 1 {
		t.Errorf("short text = %q", got)
	}
}

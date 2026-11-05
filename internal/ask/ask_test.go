package ask

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

type fakeChat struct {
	answer   string
	messages []models.Message
}

func (f *fakeChat) Chat(_ context.Context, _ string, messages []models.Message, _ models.ChatOptions) (string, error) {
	f.messages = messages
	return f.answer, nil
}

func newIndex(t *testing.T) *index.Index {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"aviso.eml":          "From: Ana <ana@example.com>\nSubject: Plano\nDate: Thu, 30 Oct 2026 07:51:00 +0100\n\nLa centralizacion esta en el sotano -2, junto al cuarto de contadores.\n",
		"references/ley.txt": "Articulo 17. Instalar un punto de recarga en el sotano requiere comunicacion previa.",
		"notes/estado.md":    "Nota: creo que el sotano -2 es el correcto, falta confirmarlo.",
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
	x, err := index.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	if _, err := x.Build(context.Background(), index.BuildOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	return x
}

func TestRun(t *testing.T) {
	x := newIndex(t)
	chat := &fakeChat{answer: "Está en el sótano -2 [1]. La ley pide comunicación previa [2, 1]. Dato inventado [7]."}
	res, err := Run(context.Background(), Options{Question: "¿Dónde está el sotano?", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("sources = %+v", res.Sources)
	}
	// Notes are not sources unless asked.
	for _, s := range res.Sources {
		if s.Role == index.RoleNote {
			t.Errorf("note used as source: %+v", s)
		}
	}
	if !slices.Equal(res.Cited, []int{1, 2}) || !slices.Equal(res.Unknown, []int{7}) {
		t.Errorf("cited %v unknown %v", res.Cited, res.Unknown)
	}
	if len(chat.messages) != 2 || chat.messages[0].Role != "system" || !strings.Contains(chat.messages[0].Content, "language of the question") {
		t.Fatalf("messages = %+v", chat.messages)
	}
	user := chat.messages[1].Content
	for _, want := range []string{`(case) aviso.eml, email "Plano"`, "Sender: Ana <ana@example.com>", "(reference) ley.txt", "Question: ¿Dónde está el sotano?"} {
		if !strings.Contains(user, want) {
			t.Errorf("user message has no %q:\n%s", want, user)
		}
	}

	res, err = Run(context.Background(), Options{Question: "sotano", WithNotes: true, Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || len(res.Sources) != 3 {
		t.Fatalf("with notes = %+v, %v", res, err)
	}
	if !strings.Contains(chat.messages[1].Content, "(note) note notes/estado.md") {
		t.Error("note not marked as note")
	}
}

func TestRunNoHits(t *testing.T) {
	x := newIndex(t)
	chat := &fakeChat{answer: "x"}
	res, err := Run(context.Background(), Options{Question: "zzzz", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || len(res.Sources) != 0 || chat.messages != nil || !strings.Contains(res.Answer, "no text") {
		t.Errorf("no hits = %+v, %v", res, err)
	}
}

func TestCitations(t *testing.T) {
	cited, unknown := citations("a [3] b [1,2] c [2][3] d [0] e [12] f [x] g [ 4 ]", 5)
	if !slices.Equal(cited, []int{1, 2, 3}) || !slices.Equal(unknown, []int{0, 12}) {
		t.Errorf("cited %v unknown %v", cited, unknown)
	}
}

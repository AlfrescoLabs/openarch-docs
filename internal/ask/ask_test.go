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

// fakeChat returns its answers in order, and repeats the last one.
type fakeChat struct {
	answers          []string
	calls            int
	expands, reranks int
	messages         []models.Message
}

func (f *fakeChat) Chat(_ context.Context, _ string, messages []models.Message, _ models.ChatOptions) (string, error) {
	// The query expansion and rerank steps get their own fixed replies.
	if text, ok := messages[0].Content.(string); ok && len(messages) == 1 {
		switch {
		case strings.Contains(text, "Write 3 search queries"):
			f.expands++
			return "centralizacion sotano\n- cuarto de contadores\n", nil
		case strings.Contains(text, "Which passages hold facts"):
			f.reranks++
			return "2, 1", nil
		}
	}
	f.messages = messages
	a := f.answers[min(f.calls, len(f.answers)-1)]
	f.calls++
	return a, nil
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
	chat := &fakeChat{answers: []string{"Está en el sótano -2 [1]. La ley pide comunicación previa [2, 1]. Dato inventado [7]."}}
	res, err := Run(context.Background(), Options{Question: "¿Dónde está el sotano?", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil {
		t.Fatal(err)
	}
	// The email, the reference and the note, last and marked as a note.
	if len(res.Sources) != 3 || res.Sources[2].Role != index.RoleNote {
		t.Fatalf("sources = %+v", res.Sources)
	}
	if !slices.Equal(res.Cited, []int{1, 2}) || !slices.Equal(res.Unknown, []int{7}) {
		t.Errorf("cited %v unknown %v", res.Cited, res.Unknown)
	}
	if len(chat.messages) != 2 || chat.messages[0].Role != "system" || !strings.Contains(chat.messages[0].Content.(string), "language of the question") {
		t.Fatalf("messages = %+v", chat.messages)
	}
	user := chat.messages[1].Content.(string)
	for _, want := range []string{`role="case" file="aviso.eml, email 'Plano'`, `sender="Ana (ana@example.com)"`, `role="reference" file="ley.txt"`, "Question: ¿Dónde está el sotano?", "Answer in Spanish."} {
		if !strings.Contains(user, want) {
			t.Errorf("user message has no %q:\n%s", want, user)
		}
	}

	if !strings.Contains(chat.messages[1].Content.(string), `role="note" file="note notes/estado.md"`) {
		t.Error("note not marked as note")
	}
	res, err = Run(context.Background(), Options{Question: "sotano", NoNotes: true, Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || len(res.Sources) != 2 {
		t.Fatalf("no notes = %+v, %v", res, err)
	}
}

func TestRunNoHits(t *testing.T) {
	x := newIndex(t)
	chat := &fakeChat{answers: []string{"x"}}
	res, err := Run(context.Background(), Options{Question: "zzzz", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || len(res.Sources) != 0 || chat.messages != nil || !strings.Contains(res.Answer, "no text") {
		t.Errorf("no hits = %+v, %v", res, err)
	}
}

func TestRunRetry(t *testing.T) {
	x := newIndex(t)
	chat := &fakeChat{answers: []string{"[1] [2]", "Está en el sótano -2 [1]."}}
	res, err := Run(context.Background(), Options{Question: "sotano", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || chat.calls != 2 || res.Answer != "Está en el sótano -2 [1]." || res.Warning != "" {
		t.Fatalf("retry = %+v, %v, calls %d", res, err, chat.calls)
	}
	if last := chat.messages[len(chat.messages)-1]; last.Role != "user" || !strings.Contains(last.Content.(string), "no sentences") {
		t.Errorf("retry message = %+v", last)
	}
	chat = &fakeChat{answers: []string{"[1]"}}
	res, err = Run(context.Background(), Options{Question: "sotano", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || chat.calls != 2 || !strings.Contains(res.Warning, "--profile large") {
		t.Errorf("no answer = %+v, %v", res, err)
	}
}

func TestSameFileIsOneSource(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("El sotano tiene contadores y cuadros electricos. ", 40)
	if err := os.WriteFile(filepath.Join(root, "largo.txt"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	x, err := index.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	if _, err := x.Build(context.Background(), index.BuildOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	chat := &fakeChat{answers: []string{"Hay contadores en el sotano [1]."}}
	res, err := Run(context.Background(), Options{Question: "sotano contadores", Index: x, Chat: chat, ChatModel: "m"})
	if err != nil || len(res.Sources) != 1 || !strings.Contains(chat.messages[1].Content.(string), "(part 2 of source 1)") {
		t.Errorf("sources = %+v, %v", res.Sources, err)
	}
}

func TestCheckCitations(t *testing.T) {
	sources := []string{
		"Alarroya contesta: las fotos de la pagina seis no las hemos aportado nosotros; las hacen los tecnicos de la distribuidora durante las visitas.",
		"Solicitud de suministro registrada con el expediente y la direccion del garaje.",
	}
	answer := "Alarroya dice que no aportaron las fotos, que hacen los tecnicos durante las visitas [2]. El expediente esta registrado [2]."
	got := checkCitations(answer, sources)
	if len(got) != 1 || got[0].Sentence != 1 || got[0].Better != 1 || !slices.Equal(got[0].Cited, []int{2}) {
		t.Errorf("checks = %+v", got)
	}
	if got := checkCitations("Alarroya dice que no aportaron las fotos durante las visitas [1].", sources); len(got) != 0 {
		t.Errorf("right citation flagged: %+v", got)
	}
}

func TestRetrieveSteps(t *testing.T) {
	x := newIndex(t)
	chat := &fakeChat{answers: []string{"Está en el sótano -2 [1]."}}
	if _, err := Run(context.Background(), Options{Question: "sotano", Index: x, Chat: chat, ChatModel: "m", Expand: true, Rerank: true}); err != nil {
		t.Fatal(err)
	}
	// Few candidates: no rerank is needed.
	if chat.expands != 1 || chat.reranks != 0 {
		t.Errorf("expands %d, reranks %d", chat.expands, chat.reranks)
	}
	chat = &fakeChat{answers: []string{"x [1]."}}
	if _, err := Run(context.Background(), Options{Question: "sotano", Index: x, Chat: chat, ChatModel: "m", Limit: 1, Expand: true, Rerank: true}); err != nil {
		t.Fatal(err)
	}
	if chat.reranks != 1 {
		t.Errorf("reranks %d with more candidates than the limit", chat.reranks)
	}
	chat = &fakeChat{answers: []string{"x [1]."}}
	if _, err := Run(context.Background(), Options{Question: "sotano", Index: x, Chat: chat, ChatModel: "m", Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if chat.expands != 0 || chat.reranks != 0 {
		t.Errorf("steps run when turned off: %d, %d", chat.expands, chat.reranks)
	}
}

func TestCitations(t *testing.T) {
	cited, unknown := citations("a [3] b [1,2] c [2][3] d [0] e [12] f [x] g [ 4 ]", 5)
	if !slices.Equal(cited, []int{1, 2, 3}) || !slices.Equal(unknown, []int{0, 12}) {
		t.Errorf("cited %v unknown %v", cited, unknown)
	}
}

// A thread file quotes messages from other people: each message is its own
// source with its own sender, so the model does not take a quoted writer
// for the writer of the email.
func TestSourcePerMessage(t *testing.T) {
	root := t.TempDir()
	thread := "From: Ana <ana@example.com>\nTo: Luis <luis@example.org>\nSubject: Re: Plano\nDate: Fri, 31 Oct 2026 09:00:00 +0100\n\n" +
		"Hola Luis, el plano del sotano esta bien.\n\n" +
		"El jue, 30 oct 2026 a las 18:00, Luis <luis@example.org> escribió:\n" +
		"> Hola Ana, te envio el plano del sotano con la centralizacion.\n"
	if err := os.WriteFile(filepath.Join(root, "hilo.eml"), []byte(thread), 0o644); err != nil {
		t.Fatal(err)
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
	chat := &fakeChat{answers: []string{"Luis envió el plano [2]."}}
	res, err := Run(context.Background(), Options{Question: "plano sotano", Index: x, Chat: chat, ChatModel: "m", NoNotes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 {
		t.Fatalf("sources = %+v", res.Sources)
	}
	prompt, _ := chat.messages[1].Content.(string)
	for _, want := range []string{`sender="Ana (ana@example.com)"`, `sender="Luis (luis@example.org)"`, `date="2026-10-30T18:00"`} {
		if strings.Count(prompt, want) != 1 {
			t.Errorf("prompt has %q %d times:\n%s", want, strings.Count(prompt, want), prompt)
		}
	}
}

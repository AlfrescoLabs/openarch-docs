package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"

	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

func TestParse(t *testing.T) {
	qs, err := Parse(strings.NewReader("# set\nQ: ¿Quién?\nE: instalador | Rubén; correo\nN: cliente\nF: lo envió el instalador\nF: por correo\n\nQ: ¿Catastro?\nP: ¿Y antes?\nS: needs the web\n"))
	if err != nil || len(qs) != 2 {
		t.Fatalf("Parse = %+v, %v", qs, err)
	}
	if len(qs[0].Expect) != 2 || qs[0].Expect[0][1] != "Rubén" || qs[0].Forbid[0] != "cliente" || qs[1].Skip != "needs the web" ||
		len(qs[0].Facts) != 2 || qs[0].Facts[1] != "por correo" || len(qs[1].Before) != 1 || qs[1].Before[0] != "¿Y antes?" {
		t.Errorf("questions = %+v", qs)
	}
	for _, bad := range []string{"E: x\n", "Q: a\nX: y\n", "Q: a\nnot a key\n"} {
		if _, err := Parse(strings.NewReader(bad)); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestScore(t *testing.T) {
	q := Question{Expect: [][]string{{"instalador", "ruben"}, {"correo"}, {"12/03"}}, Forbid: []string{"cliente envió"}}
	for answer, want := range map[string]string{
		"Lo envió Rubén por correo el 12/03.":        Pass,
		"Lo envió el INSTALADOR por correo.":         Partial,
		"No consta.":                                 Fail,
		"El cliente envió el correo el 12/03, Rubén": Fail,
	} {
		if got, _, _ := Score(q, answer); got != want {
			t.Errorf("Score(%q) = %s, want %s", answer, got, want)
		}
	}
}

func TestJudgeScore(t *testing.T) {
	for reply, want := range map[string]int{
		`{"facts":[{"fact":"a","stated":true},{"fact":"b","stated":true}]}`:                                     2,
		"Here:\n```json\n{\"facts\":[{\"fact\":\"a\",\"stated\":true},{\"fact\":\"b\",\"stated\":false}]}\n```": 1,
		`{"facts":[{"fact":"a","stated":true},{"fact":"b","stated":true,"contradicted":true}]}`:                 0,
		`{"facts":[{"fact":"a","stated":false},{"fact":"b","stated":false},{"fact":"c","stated":true}]}`:        0,
		"2": -1,
	} {
		if got, _ := judgeScore(reply, nil); got != want {
			t.Errorf("judgeScore(%q) = %d, want %d", reply, got, want)
		}
	}
}

func TestJudgeScoreFixedFacts(t *testing.T) {
	want := []string{"a", "b", "c"}
	// The judge checked only two of the three facts: the third counts as not stated.
	if got, _ := judgeScore(`{"facts":[{"fact":"a","stated":true},{"fact":"b","stated":true}]}`, want); got != 1 {
		t.Errorf("score with a fact left out = %d, want 1", got)
	}
	all := `{"facts":[{"fact":"a","stated":true},{"fact":"b","stated":true},{"fact":"c","stated":true}]}`
	if got, _ := judgeScore(all, want); got != 2 {
		t.Errorf("score with all facts = %d, want 2", got)
	}
}

func TestJudgeUsesFacts(t *testing.T) {
	c := &fakeJudge{reply: `{"facts":[{"fact":"x","stated":true}]}`}
	r := Result{Question: Question{Text: "q", Facts: []string{"fact one", "fact two"}}, Answer: "a"}
	rep := &Report{}
	if err := judge(context.Background(), Options{Judge: c}, &r, rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.prompt, "- fact one\n- fact two") || strings.Contains(c.prompt, "Reference answer") {
		t.Errorf("prompt = %q", c.prompt)
	}
	if r.Judge != 1 || rep.JudgeMax != 2 {
		t.Errorf("judge = %d, max %d", r.Judge, rep.JudgeMax)
	}
}

type fakeJudge struct{ reply, prompt string }

func (f *fakeJudge) Chat(_ context.Context, _ string, msgs []models.Message, _ models.ChatOptions) (string, error) {
	f.prompt, _ = msgs[len(msgs)-1].Content.(string)
	return f.reply, nil
}

// historyChat answers every question and records the messages of each answer.
type historyChat struct{ calls [][]models.Message }

func (h *historyChat) Chat(_ context.Context, _ string, msgs []models.Message, _ models.ChatOptions) (string, error) {
	if text, _ := msgs[0].Content.(string); len(msgs) == 1 && strings.Contains(text, "Rewrite the new question") {
		return "¿Dónde está el sotano?", nil
	}
	h.calls = append(h.calls, msgs)
	return "En el sótano -2 [1].", nil
}

func TestAskInConversation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "nota.txt"), []byte("La centralizacion esta en el sotano -2, junto al cuarto de contadores."), 0o644); err != nil {
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
	chat := &historyChat{}
	q := Question{Text: "¿Y dónde está?", Before: []string{"¿Qué hay en el sotano?", "¿Y la centralizacion?"}}
	res, err := askInConversation(context.Background(), ask.Options{Index: x, Chat: chat, ChatModel: "m", NoNotes: true}, q)
	if err != nil {
		t.Fatal(err)
	}
	// Two earlier questions, then the question with both turns before it.
	if len(chat.calls) != 3 || len(chat.calls[2]) != 6 || chat.calls[2][3].Content != "¿Y la centralizacion?" {
		t.Fatalf("%d calls, last has %d messages", len(chat.calls), len(chat.calls[len(chat.calls)-1]))
	}
	if res.Question != "¿Y dónde está?" || res.Standalone != "¿Dónde está el sotano?" {
		t.Errorf("result = %q, %q", res.Question, res.Standalone)
	}
}

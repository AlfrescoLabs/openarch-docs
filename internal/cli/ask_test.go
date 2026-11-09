package cli

import (
	"strings"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
)

func TestAskWriteText(t *testing.T) {
	r := askResult{&ask.Result{
		Answer: "The plan was sent on 4 November [1].",
		Model:  "ai/test",
		Sources: []ask.Source{
			{N: 1, Cite: "plan.png", Role: "case", Path: "extracted/a/plan.png"},
			{N: 2, Cite: "mail", Role: "case", Path: "extracted/b/message.txt"},
			{N: 3, Cite: "chat", Role: "case", Path: "extracted/c/chat.json"},
		},
		Cited:  []int{1},
		Checks: []ask.Check{{Sentence: 1, Cited: []int{1}, Better: 3}},
	}}
	var b strings.Builder
	if err := r.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"extracted/a/plan.png", "extracted/c/chat.json", "Read, not cited: [2] ("} {
		if !strings.Contains(out, want) {
			t.Errorf("output has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "extracted/b/message.txt") {
		t.Errorf("output lists the path of an uncited source:\n%s", out)
	}
}

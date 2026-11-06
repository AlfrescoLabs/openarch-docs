package eval

import (
	"os"
	"testing"
)

// The question set of the sample case parses, and every question has key
// facts for the judge.
func TestSampleQuestions(t *testing.T) {
	f, err := os.Open("../../examples/agent/sample/questions.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	qs, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) < 10 {
		t.Errorf("%d questions, want at least 10", len(qs))
	}
	for _, q := range qs {
		if len(q.Facts) == 0 || len(q.Expect) == 0 {
			t.Errorf("line %d: question without facts or expected words: %s", q.Line, q.Text)
		}
	}
}

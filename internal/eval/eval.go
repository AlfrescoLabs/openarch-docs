// Package eval runs a set of questions through ask and checks each answer
// against the words it must and must not hold. It measures how well the
// plugin answers the questions of a real case.
package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/AlfrescoLabs/openarch-docs/internal/ask"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

// Question is one question of a set.
type Question struct {
	Line int    `json:"line"`
	Text string `json:"question"`
	// Expect are groups of words; each group needs one of its words in the answer.
	Expect [][]string `json:"expect,omitempty"`
	// Forbid are words that must not be in the answer.
	Forbid []string `json:"forbid,omitempty"`
	// Skip, when set, is why the question is not asked.
	Skip string `json:"skip,omitempty"`
	// Reference is a correct answer, for the judge model.
	Reference string `json:"reference,omitempty"`
	// Facts are the key facts a right answer states. With facts, the judge
	// checks each one; without, it lists the facts of Reference itself.
	Facts []string `json:"facts,omitempty"`
}

// Parse reads a question set:
//
//	# a comment
//	Q: the question
//	E: word | other word; second group
//	N: word that must not appear
//	S: reason to skip
//	A: a correct answer, for the judge model
//	F: a key fact of a right answer (one per line)
//
// Each Q starts a question. Words are compared in lower case, without accents.
func Parse(r io.Reader) ([]Question, error) {
	var qs []Question
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || len(key) != 1 {
			return nil, fmt.Errorf("line %d: want a line that starts with Q, E, N, S, A or F and a colon", n)
		}
		value = strings.TrimSpace(value)
		if key == "Q" {
			qs = append(qs, Question{Line: n, Text: value})
			continue
		}
		if len(qs) == 0 {
			return nil, fmt.Errorf("line %d: %s line before the first question", n, key)
		}
		q := &qs[len(qs)-1]
		switch key {
		case "E":
			for _, group := range strings.Split(value, ";") {
				var words []string
				for _, w := range strings.Split(group, "|") {
					if w = strings.TrimSpace(w); w != "" {
						words = append(words, w)
					}
				}
				if len(words) > 0 {
					q.Expect = append(q.Expect, words)
				}
			}
		case "N":
			for _, w := range strings.Split(value, ";") {
				if w = strings.TrimSpace(w); w != "" {
					q.Forbid = append(q.Forbid, w)
				}
			}
		case "S":
			q.Skip = value
		case "A":
			q.Reference = strings.TrimSpace(q.Reference + " " + value)
		case "F":
			if value != "" {
				q.Facts = append(q.Facts, value)
			}
		default:
			return nil, fmt.Errorf("line %d: unknown key %q", n, key)
		}
	}
	return qs, sc.Err()
}

// Results of a question.
const (
	Pass    = "pass"
	Partial = "partial"
	Fail    = "fail"
	Skipped = "skip"
)

// Result is the outcome of one question.
type Result struct {
	Question
	Status string `json:"status"`
	Answer string `json:"answer,omitempty"`
	// Missing are the expected groups the answer lacks; Found the forbidden words it has.
	Missing []string `json:"missing,omitempty"`
	Found   []string `json:"found,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Seconds float64  `json:"seconds"`
	Error   string   `json:"error,omitempty"`
	// Judge is the score of the judge model against the reference: 2 right,
	// 1 partly right, 0 wrong; -1 when there is no judge or no reference.
	Judge int `json:"judge"`
	// JudgeMissed are the key facts the judge did not find in the answer.
	JudgeMissed []string `json:"judgeMissed,omitempty"`
}

// Report is the outcome of a set.
type Report struct {
	Model   string   `json:"model"`
	Results []Result `json:"results"`
	Pass    int      `json:"pass"`
	Partial int      `json:"partial"`
	Fail    int      `json:"fail"`
	Skip    int      `json:"skip"`
	// JudgeScore is the sum of the judge scores, out of JudgeMax.
	JudgeScore int `json:"judgeScore"`
	JudgeMax   int `json:"judgeMax"`
}

// Options configure a run.
type Options struct {
	Ask ask.Options // without Question
	// Judge and JudgeModel score answers against the references. Nil: no judge.
	Judge      ask.Chatter
	JudgeModel string
	// JudgeThink lets the judge think before it replies: slower, steadier.
	JudgeThink bool
	Progress   io.Writer
}

// judgePrompt asks the judge to check the key facts of the reference
// answer one by one. The score is computed from its reply, not asked for:
// a judge asked for one number is too lenient.
const judgePrompt = `You check an answer to a question about a case file against a reference answer that is correct.

1. List the key facts of the reference answer: the facts that answer the question. At most 5.
2. For each fact, say if the answer states it (same meaning, other words are fine), and if the answer says something that contradicts it.

Reply with JSON only, in this form:
{"facts": [{"fact": "...", "stated": true, "contradicted": false}]}

Question: %s

Reference answer: %s

Answer to check: %s`

// factsPrompt asks the judge to check fixed key facts, written by hand.
// The judge does not choose the facts, so its scores are steadier.
const factsPrompt = `You check an answer to a question about a case file against a list of key facts that are correct.

For each fact, say if the answer states it (same meaning, other words are fine), and if the answer says something that contradicts it. Keep the facts in the same order and with the same words.

Reply with JSON only, in this form:
{"facts": [{"fact": "...", "stated": true, "contradicted": false}]}

Question: %s

Key facts:
%s

Answer to check: %s`

// judgment is the reply of the judge.
type judgment struct {
	Facts []struct {
		Fact         string `json:"fact"`
		Stated       bool   `json:"stated"`
		Contradicted bool   `json:"contradicted"`
	} `json:"facts"`
}

// judgeScore turns a judge reply into a score: 0 with any contradiction, 2
// with at least 80% of the key facts stated, 1 with at least 40%, else 0. It
// returns -1 for a reply that is not a judgment. With want facts, the judge
// must check all of them: a fact it leaves out counts as not stated.
func judgeScore(reply string, want []string) (int, []string) {
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return -1, nil
	}
	var j judgment
	if json.Unmarshal([]byte(reply[start:end+1]), &j) != nil || len(j.Facts) == 0 {
		return -1, nil
	}
	stated := 0
	var missed []string
	for _, f := range j.Facts {
		if f.Contradicted {
			return 0, []string{"contradicts: " + f.Fact}
		}
		if f.Stated {
			stated++
		} else {
			missed = append(missed, f.Fact)
		}
	}
	total := max(len(j.Facts), len(want))
	switch {
	case 5*stated >= 4*total:
		return 2, missed
	case 5*stated >= 2*total:
		return 1, missed
	}
	return 0, missed
}

// Run asks every question of the set.
func Run(ctx context.Context, qs []Question, opts Options) (*Report, error) {
	rep := &Report{Model: opts.Ask.ChatModel, Results: []Result{}}
	for i, q := range qs {
		r := Result{Question: q, Judge: -1}
		if q.Skip != "" {
			r.Status = Skipped
			rep.Skip++
			rep.Results = append(rep.Results, r)
			continue
		}
		if opts.Progress != nil {
			_, _ = fmt.Fprintf(opts.Progress, "[%d/%d] %s\n", i+1, len(qs), q.Text)
		}
		o := opts.Ask
		o.Question = q.Text
		start := time.Now()
		res, err := ask.Run(ctx, o)
		r.Seconds = float64(time.Since(start).Milliseconds()) / 1000
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			r.Status, r.Error = Fail, err.Error()
			rep.Fail++
			rep.Results = append(rep.Results, r)
			continue
		}
		r.Answer = res.Answer
		for _, n := range res.Cited {
			r.Sources = append(r.Sources, res.Sources[n-1].Cite)
		}
		r.Status, r.Missing, r.Found = Score(q, res.Answer)
		if err := judge(ctx, opts, &r, rep); err != nil {
			return nil, err
		}
		switch r.Status {
		case Pass:
			rep.Pass++
		case Partial:
			rep.Partial++
		default:
			rep.Fail++
		}
		rep.Results = append(rep.Results, r)
	}
	return rep, nil
}

// judge scores one answer against its reference, when there is a judge.
func judge(ctx context.Context, opts Options, r *Result, rep *Report) error {
	if opts.Judge == nil || (r.Reference == "" && len(r.Facts) == 0) || r.Answer == "" {
		return nil
	}
	prompt := fmt.Sprintf(judgePrompt, r.Text, r.Reference, r.Answer)
	if len(r.Facts) > 0 {
		prompt = fmt.Sprintf(factsPrompt, r.Text, "- "+strings.Join(r.Facts, "\n- "), r.Answer)
	}
	maxTokens := 1200
	if opts.JudgeThink {
		maxTokens = 16000
	}
	reply, err := opts.Judge.Chat(ctx, opts.JudgeModel, []models.Message{{Role: "user", Content: prompt}},
		models.ChatOptions{Temperature: 0, Seed: 42, MaxTokens: maxTokens, Think: opts.JudgeThink})
	if errors.Is(err, models.ErrNoAnswer) {
		// No score for this answer; the others still count.
		return nil
	}
	if err != nil {
		return err
	}
	r.Judge, r.JudgeMissed = judgeScore(reply, r.Facts)
	if r.Judge >= 0 {
		rep.JudgeScore += r.Judge
		rep.JudgeMax += 2
	}
	return nil
}

// Rejudge scores the answers of a report again, without asking again: to
// compare judges, or to judge a report made with --no-judge.
func Rejudge(ctx context.Context, old *Report, qs []Question, opts Options) (*Report, error) {
	byText := map[string]Question{}
	for _, q := range qs {
		byText[q.Text] = q
	}
	rep := &Report{Model: old.Model, Results: []Result{}, Pass: old.Pass, Partial: old.Partial, Fail: old.Fail, Skip: old.Skip}
	for _, r := range old.Results {
		r.Judge, r.JudgeMissed = -1, nil
		if q, ok := byText[r.Text]; ok {
			r.Reference, r.Facts = q.Reference, q.Facts
		}
		if err := judge(ctx, opts, &r, rep); err != nil {
			return nil, err
		}
		rep.Results = append(rep.Results, r)
	}
	return rep, nil
}

// Score checks an answer: pass when every expected group is there and no
// forbidden word is; partial when at least half of the groups are there and
// no forbidden word is; else fail.
func Score(q Question, answer string) (status string, missing, found []string) {
	a := fold(answer)
	for _, group := range q.Expect {
		ok := false
		for _, w := range group {
			if strings.Contains(a, fold(w)) {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, strings.Join(group, " | "))
		}
	}
	for _, w := range q.Forbid {
		if strings.Contains(a, fold(w)) {
			found = append(found, w)
		}
	}
	switch {
	case len(found) > 0:
		return Fail, missing, found
	case len(missing) == 0:
		return Pass, nil, nil
	case 2*len(missing) <= len(q.Expect):
		return Partial, missing, nil
	}
	return Fail, missing, nil
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

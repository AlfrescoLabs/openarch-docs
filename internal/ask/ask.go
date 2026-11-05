// Package ask answers a question about the case folder with a local chat
// model: it searches the index, gives the model the numbered sources, and
// checks that the answer cites only those sources.
package ask

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

// Chatter answers chat messages.
type Chatter interface {
	Chat(ctx context.Context, model string, messages []models.Message, opts models.ChatOptions) (string, error)
}

// Options configure a question.
type Options struct {
	Question string
	// Limit is the number of sources the model reads.
	Limit int
	// NoNotes leaves the notes out of the sources. By default up to maxNotes
	// notes join, marked as interpretations: they hold earlier work.
	NoNotes bool
	// Think lets the model think before it answers: slower, sometimes better.
	Think      bool
	Index      *index.Index
	Embedder   index.Embedder
	EmbedModel string
	Chat       Chatter
	ChatModel  string
}

// Source is one numbered source the model read.
type Source struct {
	N      int    `json:"n"`
	Cite   string `json:"cite"`
	Path   string `json:"path"`
	Origin string `json:"origin"`
	Page   int    `json:"page,omitempty"`
	Role   string `json:"role"`
}

// Result is an answer with its sources.
type Result struct {
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Model    string   `json:"model"`
	Sources  []Source `json:"sources"`
	// Cited are the source numbers the answer cites.
	Cited []int `json:"cited"`
	// Unknown are numbers the answer cites that are not sources. They mean
	// the model made up a citation.
	Unknown []int `json:"unknown"`
	// Checks are sentences whose words match another source much better than
	// the cited one: the model may have cited the wrong source.
	Checks []Check `json:"checks"`
	// Warning tells that the answer is not usable, such as an answer with
	// only citations.
	Warning string `json:"warning,omitempty"`
}

// retryPrompt asks again when the answer has no sentences.
const retryPrompt = "Your answer has no sentences. Write 1 to 5 full sentences in the language of the question, each followed by its source numbers. If the sources do not hold the answer, write that in one sentence."

// maxNotes is the number of notes that join the sources.
const maxNotes = 3

// instruction is the system message. The answer language follows the question.
const instruction = `You answer questions about a case file. Use only the sources in the user message. Each source is between <source n="N" ...> and </source>.

Rules:
- Answer in the language of the question.
- Every fact needs the number of the source whose text holds it, in square brackets, like [2]. An email often quotes other emails: cite the source where you read the words, not the source of the writer.
- If the sources do not hold the answer, say so plainly, and say which document would hold it.
- Keep facts apart from interpretations. Mark an interpretation as such.
- Sources marked "note" are earlier notes of the user: interpretations, not evidence. Prefer case sources.
- Sources marked "reference" are laws or rules, not facts of the case.
- Do not say that a person or a company is at fault unless a source says so.
- When the question asks where a document is, give its file name and its path from the source.
- "Last" or "latest" means the newest date among the sources.
- When a fact comes from an email or a chat, say who wrote it. Never write "we" or "us" for a writer.

Format: 1 to 5 plain sentences that answer the question. Put the source numbers after each sentence, like "The meter room is in the basement [3]."`

// Run answers the question.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 12
	}
	hits, err := opts.Index.Search(ctx, index.SearchOptions{
		Query: opts.Question, Limit: opts.Limit, Roles: []string{casefile.RoleCase, casefile.RoleReference}, Embedder: opts.Embedder, Model: opts.EmbedModel,
	})
	if err != nil {
		return nil, err
	}
	if !opts.NoNotes {
		notes, err := opts.Index.Search(ctx, index.SearchOptions{
			Query: opts.Question, Limit: maxNotes, Roles: []string{index.RoleNote}, Embedder: opts.Embedder, Model: opts.EmbedModel,
		})
		if err != nil {
			return nil, err
		}
		hits = append(hits, notes...)
	}
	// "The last email" needs the newest emails, which search does not rank.
	if lang.AsksLatest(opts.Question) {
		latest, err := opts.Index.Latest(ctx, 3)
		if err != nil {
			return nil, err
		}
		hits = append(latest, hits...)
	}
	res := &Result{Question: opts.Question, Model: opts.ChatModel, Sources: []Source{}, Cited: []int{}, Unknown: []int{}, Checks: []Check{}}
	if len(hits) == 0 {
		res.Answer = "The index has no text for this question. Run 'openarch docs index', or add the documents that hold the answer."
		return res, nil
	}
	// Pieces of the same file become one source, in rank order.
	type group struct {
		hit   index.Hit
		texts []string
	}
	var groups []*group
	byPath := map[string]*group{}
	for _, h := range hits {
		if g, ok := byPath[h.Path]; ok {
			g.texts = append(g.texts, strings.TrimSpace(h.Text))
			continue
		}
		g := &group{hit: h, texts: []string{strings.TrimSpace(h.Text)}}
		byPath[h.Path] = g
		groups = append(groups, g)
	}
	var b strings.Builder
	var sourceTexts []string
	fmt.Fprintf(&b, "Question: %s\n\n", opts.Question)
	for i, g := range groups {
		h := g.hit
		s := Source{N: i + 1, Cite: h.Cite, Path: h.Path, Origin: h.Origin, Page: h.Page, Role: h.Role}
		res.Sources = append(res.Sources, s)
		fmt.Fprintf(&b, "<source n=\"%d\" role=\"%s\" file=\"%s\" path=\"%s\"", s.N, s.Role, attr(s.Cite), attr(s.Path))
		if h.Sender != "" {
			fmt.Fprintf(&b, " sender=\"%s\"", attr(h.Sender))
		}
		if h.Date != "" {
			fmt.Fprintf(&b, " date=\"%s\"", attr(h.Date))
		}
		b.WriteString(">\n")
		for j, t := range g.texts {
			if len(g.texts) > 1 {
				fmt.Fprintf(&b, "(part %d of source %d)\n", j+1, s.N)
			}
			b.WriteString(t + "\n")
		}
		b.WriteString("</source>\n\n")
		sourceTexts = append(sourceTexts, strings.Join(g.texts, "\n"))
	}
	fmt.Fprintf(&b, "Question: %s", opts.Question)
	// Models often answer in English when told to use "the language of the
	// question", so name the language.
	if l, ok := lang.Detect(opts.Question); ok {
		fmt.Fprintf(&b, "\n\nAnswer in %s.", l.Name)
	}
	messages := []models.Message{
		{Role: "system", Content: instruction},
		{Role: "user", Content: b.String()},
	}
	// Temperature 0 and a fixed seed: the same question on the same case gives
	// the same answer, so runs can be compared.
	chatOpts := models.ChatOptions{Temperature: 0, Seed: 42, MaxTokens: 2048, Think: opts.Think}
	answer, err := opts.Chat.Chat(ctx, opts.ChatModel, messages, chatOpts)
	if err != nil {
		return nil, err
	}
	if !hasSentences(answer) {
		messages = append(messages, models.Message{Role: "assistant", Content: answer}, models.Message{Role: "user", Content: retryPrompt})
		if answer, err = opts.Chat.Chat(ctx, opts.ChatModel, messages, chatOpts); err != nil {
			return nil, err
		}
		if !hasSentences(answer) {
			res.Warning = "The model gave no usable answer. Read the sources below, or try a larger model with --profile large."
		}
	}
	res.Answer = answer
	res.Checks = checkCitations(answer, sourceTexts)
	res.Cited, res.Unknown = citations(answer, len(res.Sources))
	return res, nil
}

// attr makes s safe inside a double-quoted attribute.
func attr(s string) string {
	return strings.NewReplacer(`"`, "'", "\n", " ", "<", "(", ">", ")").Replace(s)
}

// Check is a sentence that may cite the wrong source.
type Check struct {
	Sentence int    `json:"sentence"`
	Text     string `json:"text"`
	Cited    []int  `json:"cited"`
	// Better is the source whose text has the most words of the sentence.
	Better int `json:"better"`
}

// sentenceEnd splits an answer into sentences after their citations.
var sentenceEnd = regexp.MustCompile(`([.!?])(\s*\[[\d,\s\]\[]*\])?\s+`)

// checkCitations compares the content words of each cited sentence with the
// text of every source. When another source holds clearly more of them than
// every cited source, the sentence is reported. The check needs no model.
func checkCitations(answer string, sources []string) []Check {
	checks := []Check{}
	words := make([]map[string]bool, len(sources))
	for i, t := range sources {
		words[i] = contentWords(t)
	}
	parts := sentenceEnd.ReplaceAllString(answer, "$1$2\x00")
	n := 0
	for _, sent := range strings.Split(parts, "\x00") {
		if strings.TrimSpace(sent) == "" {
			continue
		}
		n++
		cited, _ := citations(sent, len(sources))
		if len(cited) == 0 {
			continue
		}
		sw := contentWords(citation.ReplaceAllString(sent, " "))
		if len(sw) < 3 {
			continue
		}
		score := func(i int) int {
			c := 0
			for w := range sw {
				if words[i][w] {
					c++
				}
			}
			return c
		}
		bestCited := 0
		for _, c := range cited {
			bestCited = max(bestCited, score(c-1))
		}
		better, betterScore := 0, 0
		for i := range sources {
			if sc := score(i); sc > betterScore {
				better, betterScore = i+1, sc
			}
		}
		if !slices.Contains(cited, better) && betterScore >= 3 && betterScore*2 >= bestCited*3 {
			checks = append(checks, Check{Sentence: n, Text: strings.TrimSpace(sent), Cited: cited, Better: better})
		}
	}
	return checks
}

// contentWords returns the words of s that carry meaning: four letters or
// more, not stop words, lower case and without accents.
func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		w = foldAccents(w)
		if len([]rune(w)) >= 4 && !stopWords[w] {
			out[w] = true
		}
	}
	return out
}

var stopWords = lang.AllStopWords()

func foldAccents(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hasSentences reports whether the answer has words besides its citations.
func hasSentences(answer string) bool {
	rest := citation.ReplaceAllString(answer, " ")
	n := 0
	for _, r := range rest {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n >= 10
}

// citation matches [3] and [1, 4] and [2][5].
var citation = regexp.MustCompile(`\[(\d+(?:\s*,\s*\d+)*)\]`)

// citations returns the cited source numbers, and the cited numbers that are
// not sources, both sorted and without repeats.
func citations(answer string, sources int) (cited, unknown []int) {
	cited, unknown = []int{}, []int{}
	for _, m := range citation.FindAllStringSubmatch(answer, -1) {
		for _, part := range strings.Split(m[1], ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				continue
			}
			if n >= 1 && n <= sources {
				cited = append(cited, n)
			} else {
				unknown = append(unknown, n)
			}
		}
	}
	slices.Sort(cited)
	slices.Sort(unknown)
	return slices.Compact(cited), slices.Compact(unknown)
}

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

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/index"
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
	// WithNotes adds notes to the sources. They are interpretations, not evidence.
	WithNotes  bool
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
}

// instruction is the system message. The answer language follows the question.
const instruction = `You answer questions about a case file. Use only the numbered sources in the user message.

Rules:
- Answer in the language of the question.
- Every fact needs the number of its source in square brackets, like [2]. Use only numbers from the list.
- If the sources do not hold the answer, say so plainly, and say which document would hold it.
- Keep facts apart from interpretations. Mark an interpretation as such.
- Sources marked "note" are earlier notes of the user: interpretations, not evidence. Prefer case sources.
- Sources marked "reference" are laws or rules, not facts of the case.
- Do not say that a person or a company is at fault unless a source says so.
- Be short and plain.`

// Run answers the question.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 8
	}
	roles := []string{casefile.RoleCase, casefile.RoleReference}
	if opts.WithNotes {
		roles = append(roles, index.RoleNote)
	}
	hits, err := opts.Index.Search(ctx, index.SearchOptions{
		Query: opts.Question, Limit: opts.Limit, Roles: roles, Embedder: opts.Embedder, Model: opts.EmbedModel,
	})
	if err != nil {
		return nil, err
	}
	res := &Result{Question: opts.Question, Model: opts.ChatModel, Sources: []Source{}, Cited: []int{}, Unknown: []int{}}
	if len(hits) == 0 {
		res.Answer = "The index has no text for this question. Run 'openarch docs index', or add the documents that hold the answer."
		return res, nil
	}
	var b strings.Builder
	b.WriteString("Sources:\n\n")
	for i, h := range hits {
		s := Source{N: i + 1, Cite: h.Cite, Path: h.Path, Origin: h.Origin, Page: h.Page, Role: h.Role}
		res.Sources = append(res.Sources, s)
		fmt.Fprintf(&b, "[%d] (%s) %s\n", s.N, s.Role, s.Cite)
		if h.Sender != "" {
			fmt.Fprintf(&b, "Sender: %s\n", h.Sender)
		}
		fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(h.Text))
	}
	fmt.Fprintf(&b, "Question: %s", opts.Question)
	answer, err := opts.Chat.Chat(ctx, opts.ChatModel, []models.Message{
		{Role: "system", Content: instruction},
		{Role: "user", Content: b.String()},
	}, models.ChatOptions{Temperature: 0.1, MaxTokens: 1024})
	if err != nil {
		return nil, err
	}
	res.Answer = answer
	res.Cited, res.Unknown = citations(answer, len(res.Sources))
	return res, nil
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

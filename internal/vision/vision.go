// Package vision reads images with the local vision model: describe writes a
// text for each photo, plan and image-heavy page, so search and ask can use
// it; look asks one question about several images.
package vision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // GIF decoder for image.Decode
	"image/jpeg"
	_ "image/png" // PNG decoder for image.Decode
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/image/draw"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/extract"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/models"
)

// KindDescription is the kind of a description written by the vision model.
const KindDescription = "description"

// MethodVision is the method of a description.
const MethodVision = "vision"

const (
	// maxSide is the longest side, in pixels, of an image sent to the model.
	// gemma3 reads images at 896 x 896; more only costs time.
	maxSide = 1600
	// minPixels skips small images such as logos in email signatures.
	minPixels = 40000
)

// Chatter answers chat messages.
type Chatter interface {
	Chat(ctx context.Context, model string, messages []models.Message, opts models.ChatOptions) (string, error)
}

// ErrUnsupported means the image format cannot be read.
var ErrUnsupported = errors.New("image format not supported; use PNG, JPEG or GIF")

// Prepare reads an image and returns it ready for the model: PNG, JPEG or
// GIF, scaled down to maxSide when larger. It also returns the size in pixels.
func Prepare(data []byte) (mediaType string, out []byte, w, h int, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, 0, 0, ErrUnsupported
	}
	w, h = cfg.Width, cfg.Height
	if w <= maxSide && h <= maxSide {
		return "image/" + format, data, w, h, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, 0, 0, err
	}
	nw, nh := maxSide, h*maxSide/w
	if h > w {
		nw, nh = w*maxSide/h, maxSide
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return "", nil, 0, 0, err
	}
	return "image/jpeg", buf.Bytes(), w, h, nil
}

// describePrompt is the instruction for one image. Both %s are the language name.
const describePrompt = `You describe an image from a case file, so that a person can find it with a text search and knows what it shows without opening it.

Use these four headings, in this order:
Type: photo, plan, map, screenshot, scanned document, form, signature, logo, or other.
Text: every text you can read in the image, exactly as written. Write "none" when there is no text.
Content: what the image shows: places, rooms, objects, marks, arrows, colors, and how they relate. Do not name people unless their name is written.
Location: every address, floor, room, door or number that tells where this is. Write "none" when there is none.

Do not guess. When something is unclear, say that it is unclear.
Start with "Type:". Write no greeting and no closing offer.
Write Content and Location in %s. Keep the headings in English.`

// closing matches the offers that chat models add at the end, such as
// "Let me know if you want more detail".
var closing = regexp.MustCompile(`(?i)^\W*(let me know|do you want|would you like|i hope|feel free|if you (need|want|would))`)

// cleanDescription drops text before the first heading and closing offers.
func cleanDescription(text string) string {
	if i := strings.Index(strings.ToLower(text), "type:"); i > 0 {
		// Keep a Markdown mark such as "**" right before the heading.
		start := strings.LastIndex(text[:i], "\n") + 1
		text = text[start:]
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for len(lines) > 0 && (closing.MatchString(lines[len(lines)-1]) || strings.TrimSpace(lines[len(lines)-1]) == "") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Options configure a describe run.
type Options struct {
	Root     string
	Chat     Chatter
	Model    string
	Language lang.Language
	// AllPages also describes PDF pages without large images.
	AllPages bool
	// Force describes again images that have a description.
	Force bool
	// Max stops after this many descriptions. 0 means no limit.
	Max      int
	Progress io.Writer
}

// Statuses of an image in the result.
const (
	StatusDone        = "done"
	StatusSkipped     = "skipped"
	StatusSmall       = "small"
	StatusUnsupported = "unsupported"
	StatusFailed      = "failed"
)

// Item is one image in the result.
type Item struct {
	Path   string `json:"path"`
	From   string `json:"from"`
	Page   int    `json:"page,omitempty"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Result is the outcome of a describe run.
type Result struct {
	Model    string `json:"model"`
	Language string `json:"language"`
	Items    []Item `json:"items"`
	Done     int    `json:"done"`
	Skipped  int    `json:"skipped"`
	// Small counts images below about 200 x 200 pixels, such as logos.
	Small int `json:"small"`
	// Removed counts descriptions of text pages that were dropped.
	Removed int `json:"removed"`
	// Pending counts images left for a later run because of Max.
	Pending     int `json:"pending"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
}

type target struct {
	image  string // the image file
	from   string // the PDF of a page image, else the image
	page   int
	source string
	out    string // the description file
}

// Describe writes a description for each photo, plan and image-heavy page.
func Describe(ctx context.Context, opts Options) (*Result, error) {
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	res := &Result{Model: opts.Model, Language: opts.Language.Code, Items: []Item{}}
	// Descriptions of text pages from older runs only add noise.
	if !opts.AllPages {
		keep := map[string]bool{}
		for _, t := range targets(opts.Root, m, false) {
			keep[t.from+"#"+strconv.Itoa(t.page)] = true
		}
		if err := m.RemoveDerived(opts.Root, func(d casefile.Derived) bool {
			drop := d.Kind == KindDescription && d.Page > 0 && !keep[d.From+"#"+strconv.Itoa(d.Page)]
			if drop {
				res.Removed++
			}
			return drop
		}); err != nil {
			return nil, err
		}
		if res.Removed > 0 {
			if err := casefile.Save(opts.Root, m); err != nil {
				return nil, err
			}
		}
	}
	for _, t := range targets(opts.Root, m, opts.AllPages) {
		it := Item{Path: t.image, From: t.from, Page: t.page}
		switch {
		case !opts.Force && described(m, t):
			it.Status = StatusSkipped
		case precheck(opts.Root, t) != "":
			it.Status = precheck(opts.Root, t)
		case opts.Max > 0 && res.Done+res.Failed >= opts.Max:
			res.Pending++
			continue
		default:
			it.Status, it.Output, err = describeOne(ctx, opts, m, t)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				var me *models.ModelError
				if errors.Is(err, models.ErrUnreachable) || errors.As(err, &me) && me.Status != 500 {
					return nil, err
				}
				it.Status, it.Error = StatusFailed, err.Error()
			}
		}
		switch it.Status {
		case StatusDone:
			res.Done++
			if err := casefile.Save(opts.Root, m); err != nil {
				return nil, err
			}
		case StatusSkipped:
			res.Skipped++
		case StatusSmall:
			res.Small++
			continue
		case StatusUnsupported:
			res.Unsupported++
		case StatusFailed:
			res.Failed++
		}
		res.Items = append(res.Items, it)
	}
	return res, nil
}

// Pending returns the number of images that describe would describe.
func Pending(root string, m *casefile.Manifest) int {
	n := 0
	for _, t := range targets(root, m, false) {
		if !described(m, t) && precheck(root, t) == "" {
			n++
		}
	}
	return n
}

// textPage is the number of letters from which a page is a text page, such
// as a scanned deed: its OCR or text layer holds what matters, and a
// description only repeats it and crowds the search results.
const textPage = 600

// pageLetters returns, for each PDF page, the letters of its longest text
// (text layer or OCR), keyed by "<pdf>#<page>".
func pageLetters(root string, m *casefile.Manifest) map[string]int {
	out := map[string]int{}
	for _, d := range m.Derived {
		if d.Kind != extract.KindPageText && d.Kind != extract.KindPageOCR {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.Path)))
		if err != nil {
			continue
		}
		n := 0
		for _, r := range string(data) {
			if unicode.IsLetter(r) {
				n++
			}
		}
		key := d.From + "#" + strconv.Itoa(d.Page)
		out[key] = max(out[key], n)
	}
	return out
}

func targets(root string, m *casefile.Manifest, allPages bool) []target {
	var out []target
	letters := pageLetters(root, m)
	for _, d := range m.Derived {
		textOnly := letters[d.From+"#"+strconv.Itoa(d.Page)] >= textPage
		if d.Kind == extract.KindPageImage && (allPages || (d.Images > 0 && !textOnly)) {
			out = append(out, target{image: d.Path, from: d.From, page: d.Page, source: d.Source,
				out: strings.TrimSuffix(d.Path, path.Ext(d.Path)) + ".describe.txt"})
		}
	}
	for _, o := range m.Originals {
		if o.Kind == casefile.KindImage {
			out = append(out, target{image: o.Path, from: o.Path, source: o.Path, out: path.Join(casefile.DerivedDir(o.Path, o.SHA256), "describe.txt")})
		}
	}
	unpacked := []string{ingest.KindAttachment, ingest.KindInline, ingest.KindMedia, ingest.KindZipEntry}
	for _, d := range m.Derived {
		if slices.Contains(unpacked, d.Kind) && (strings.HasPrefix(d.MediaType, "image/") || casefile.KindOf(d.Path) == casefile.KindImage) {
			out = append(out, target{image: d.Path, from: d.Path, source: d.Source, out: d.Path + ".extract/describe.txt"})
		}
	}
	return out
}

// precheck returns StatusSmall or StatusUnsupported without calling the
// model, or "" when the image needs a description.
func precheck(root string, t target) string {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(t.image)))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	cfg, _, err := image.DecodeConfig(f)
	switch {
	case err != nil:
		return StatusUnsupported
	case t.page == 0 && cfg.Width*cfg.Height < minPixels:
		return StatusSmall
	}
	return ""
}

func described(m *casefile.Manifest, t target) bool {
	return slices.ContainsFunc(m.Derived, func(d casefile.Derived) bool {
		return d.Kind == KindDescription && d.From == t.from && d.Page == t.page
	})
}

func describeOne(ctx context.Context, opts Options, m *casefile.Manifest, t target) (status, output string, err error) {
	data, err := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(t.image)))
	if err != nil {
		return "", "", err
	}
	mt, img, _, _, err := Prepare(data)
	if errors.Is(err, ErrUnsupported) {
		return StatusUnsupported, "", nil
	}
	if err != nil {
		return "", "", err
	}
	if opts.Progress != nil {
		label := t.image
		if t.page > 0 {
			label = fmt.Sprintf("%s p.%d", t.from, t.page)
		}
		_, _ = fmt.Fprintf(opts.Progress, "Describing %s\n", label)
	}
	text, err := opts.Chat.Chat(ctx, opts.Model, []models.Message{{Role: "user", Content: []models.Part{
		models.TextPart(fmt.Sprintf(describePrompt, opts.Language.Name)), models.ImagePart(mt, img),
		models.TextPart("Write Content and Location in " + opts.Language.Name + "."),
	}}}, models.ChatOptions{Temperature: 0.1, MaxTokens: 700})
	if err != nil {
		return "", "", err
	}
	if err := m.RemoveDerived(opts.Root, func(d casefile.Derived) bool {
		return d.Kind == KindDescription && d.From == t.from && d.Page == t.page
	}); err != nil {
		return "", "", err
	}
	p, err := m.WriteDerived(opts.Root, t.out, []byte(cleanDescription(text)+"\n"), casefile.Derived{
		Source: t.source, From: t.from, Page: t.page, Kind: KindDescription, MediaType: "text/plain",
		Method: MethodVision, Model: opts.Model, Languages: []string{opts.Language.Code},
	})
	return StatusDone, p, err
}

// CaseLanguage guesses the main language of the case from its texts.
func CaseLanguage(root string) (lang.Language, bool) {
	m, err := casefile.Load(root)
	if err != nil {
		return lang.Language{}, false
	}
	var b strings.Builder
	n := 0
	for _, d := range m.Derived {
		if (d.Kind != ingest.KindBody && d.Kind != extract.KindPageText) || n >= 30 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(d.Path)))
		if err != nil {
			continue
		}
		r := []rune(string(data))
		b.WriteString(string(r[:min(len(r), 2000)]))
		b.WriteByte('\n')
		n++
	}
	return lang.Detect(b.String())
}

// LookImage is one image in a look question.
type LookImage struct {
	N     int    `json:"n"`
	Ref   string `json:"ref"`
	Path  string `json:"path"`
	Label string `json:"label"`
}

// LookResult is the answer about the images.
type LookResult struct {
	Question string      `json:"question"`
	Answer   string      `json:"answer"`
	Model    string      `json:"model"`
	Images   []LookImage `json:"images"`
}

// Look asks the vision model one question about several images. A ref is a
// path in the case folder, or "<pdf>#<page>" for a page of a PDF.
func Look(ctx context.Context, root string, chat Chatter, model, question string, refs []string) (*LookResult, error) {
	m, err := casefile.Load(root)
	if err != nil {
		return nil, err
	}
	res := &LookResult{Question: question, Model: model, Images: []LookImage{}}
	var parts []models.Part
	for i, ref := range refs {
		p, label, err := resolve(root, m, ref)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		mt, img, _, _, err := Prepare(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ref, err)
		}
		res.Images = append(res.Images, LookImage{N: i + 1, Ref: ref, Path: p, Label: label})
		parts = append(parts, models.TextPart(fmt.Sprintf("Image %d: %s", i+1, label)), models.ImagePart(mt, img))
	}
	q := "Question: " + question + "\nRefer to the images as Image 1, Image 2 and so on. Say only what you can see. When something is unclear, say so."
	if l, ok := lang.Detect(question); ok {
		q += "\nAnswer in " + l.Name + "."
	}
	parts = append(parts, models.TextPart(q))
	answer, err := chat.Chat(ctx, model, []models.Message{{Role: "user", Content: parts}}, models.ChatOptions{Temperature: 0.1, MaxTokens: 900})
	if err != nil {
		return nil, err
	}
	res.Answer = answer
	return res, nil
}

// resolve turns a ref into a path in the case folder and a label for the model.
func resolve(root string, m *casefile.Manifest, ref string) (string, string, error) {
	if pdf, page, ok := strings.Cut(ref, "#"); ok {
		n, err := strconv.Atoi(page)
		if err != nil || n < 1 {
			return "", "", fmt.Errorf("%s: the page after # must be a number from 1", ref)
		}
		pdf = clean(root, pdf)
		for _, d := range m.Derived {
			if d.Kind == extract.KindPageImage && d.From == pdf && d.Page == n {
				return d.Path, fmt.Sprintf("%s p.%d", path.Base(pdf), n), nil
			}
		}
		return "", "", fmt.Errorf("%s: no page image; run 'openarch docs extract' first", ref)
	}
	p := clean(root, ref)
	if p == "" || strings.HasPrefix(p, "../") || p == ".." {
		return "", "", fmt.Errorf("%s is not in the case folder", ref)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
		return "", "", fmt.Errorf("%s: %w", ref, err)
	}
	return p, path.Base(p), nil
}

// clean returns ref as a slash path relative to the case folder.
func clean(root, ref string) string {
	if filepath.IsAbs(ref) {
		if rel, err := filepath.Rel(root, ref); err == nil {
			ref = rel
		}
	}
	return path.Clean(filepath.ToSlash(ref))
}

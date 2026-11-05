// Package extract gets the text of PDFs and images: the PDF text layer per
// page, an image of each page, OCR for pages with little text and for images,
// and the number of large images (photos, plans) on each page.
package extract

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
	"github.com/AlfrescoLabs/openarch-docs/internal/tools"
)

// Kinds of the derived files that extract writes.
const (
	KindPageText  = "page-text"
	KindPageOCR   = "page-ocr"
	KindPageImage = "page-image"
	KindImageOCR  = "image-ocr"
)

// Methods of a text.
const (
	MethodTextLayer = "text-layer"
	MethodOCR       = "ocr"
)

// minImageShare is the part of the page from which an image counts as a
// photo or a plan, not a logo or a letterhead.
const minImageShare = 0.10

// Options configure an extract run.
type Options struct {
	Root      string
	Runner    tools.Runner
	Languages []lang.Language
	// DPI is the resolution of the page images.
	DPI int
	// MinText is the number of letters and digits below which a page gets OCR.
	MinText int
	// Force extracts again files that already have results.
	Force bool
	// Progress receives one line per file. It may be nil.
	Progress io.Writer
}

// Statuses of a file in the result.
const (
	StatusDone        = "done"
	StatusSkipped     = "skipped"
	StatusUnsupported = "unsupported"
	StatusFailed      = "failed"
)

// File is one PDF or image in the result.
type File struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Pages  int    `json:"pages,omitempty"`
	// OCRPages are the pages that got OCR because they had little text.
	OCRPages []int `json:"ocrPages,omitempty"`
	// ImagePages are the pages with large images, such as photos or plans.
	ImagePages []int  `json:"imagePages,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Result is the outcome of an extract run.
type Result struct {
	Root      string   `json:"root"`
	Languages []string `json:"languages"`
	Files     []File   `json:"files"`
	Done      int      `json:"done"`
	Skipped   int      `json:"skipped"`
	// Unsupported counts files that OCR cannot read, such as HEIC photos.
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
}

// target is a PDF or image to extract.
type target struct {
	path   string // file to read, relative to the case folder
	source string // original it comes from
	kind   string // casefile.KindPDF or casefile.KindImage
	outDir string // folder for the results
}

// Run extracts every PDF and image of the case folder: originals and files
// that ingest unpacked from emails and zips.
func Run(ctx context.Context, opts Options) (*Result, error) {
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	codes := make([]string, len(opts.Languages))
	tess := make([]string, len(opts.Languages))
	for i, l := range opts.Languages {
		codes[i], tess[i] = l.Code, l.Tesseract
	}
	res := &Result{Root: opts.Root, Languages: codes, Files: []File{}}
	for _, t := range targets(m) {
		f := File{Path: t.path, Kind: t.kind}
		switch {
		case !supported(t.path):
			f.Status = StatusUnsupported
			f.Error = "OCR cannot read " + path.Ext(t.path) + " files; convert it to PNG or JPEG"
		case hasResults(m, t.path) && !opts.Force:
			f.Status = StatusSkipped
		default:
			if opts.Progress != nil {
				_, _ = fmt.Fprintf(opts.Progress, "Extracting %s\n", t.path)
			}
			if err := m.RemoveDerived(opts.Root, func(d casefile.Derived) bool { return d.From == t.path }); err != nil {
				return nil, err
			}
			var err error
			if t.kind == casefile.KindPDF {
				err = extractPDF(ctx, opts, m, t, strings.Join(tess, "+"), codes, &f)
			} else {
				err = extractImage(ctx, opts, m, t, strings.Join(tess, "+"), codes)
			}
			f.Status = StatusDone
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				f.Status, f.Error = StatusFailed, err.Error()
			}
			if err := casefile.Save(opts.Root, m); err != nil {
				return nil, err
			}
		}
		switch f.Status {
		case StatusDone:
			res.Done++
		case StatusSkipped:
			res.Skipped++
		case StatusUnsupported:
			res.Unsupported++
		case StatusFailed:
			res.Failed++
		}
		res.Files = append(res.Files, f)
	}
	return res, nil
}

// Pending returns the number of PDFs and images that extract would read.
func Pending(m *casefile.Manifest) int {
	n := 0
	for _, t := range targets(m) {
		if supported(t.path) && !hasResults(m, t.path) {
			n++
		}
	}
	return n
}

// targets lists the PDFs and images among the originals and the files that
// ingest unpacked.
func targets(m *casefile.Manifest) []target {
	var out []target
	for _, o := range m.Originals {
		if o.Kind == casefile.KindPDF || o.Kind == casefile.KindImage {
			out = append(out, target{path: o.Path, source: o.Path, kind: o.Kind, outDir: casefile.DerivedDir(o.Path, o.SHA256)})
		}
	}
	unpacked := []string{ingest.KindAttachment, ingest.KindInline, ingest.KindMedia, ingest.KindZipEntry}
	for _, d := range m.Derived {
		if !slices.Contains(unpacked, d.Kind) {
			continue
		}
		kind := casefile.KindOf(d.Path)
		switch {
		case d.MediaType == "application/pdf":
			kind = casefile.KindPDF
		case strings.HasPrefix(d.MediaType, "image/"):
			kind = casefile.KindImage
		}
		if kind == casefile.KindPDF || kind == casefile.KindImage {
			out = append(out, target{path: d.Path, source: d.Source, kind: kind, outDir: d.Path + ".extract"})
		}
	}
	return out
}

// supported reports whether tesseract and poppler can read the file.
func supported(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".heic", ".webp":
		return false
	}
	return true
}

func hasResults(m *casefile.Manifest, p string) bool {
	return slices.ContainsFunc(m.Derived, func(d casefile.Derived) bool { return d.From == p })
}

func extractPDF(ctx context.Context, opts Options, m *casefile.Manifest, t target, tess string, codes []string, f *File) error {
	tmp, err := tempDir(opts.Root)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	input := filepath.Join(opts.Root, filepath.FromSlash(t.path))

	// First run: page count, text layer and the list of images.
	err = opts.Runner.Run(ctx, input, tmp, []tools.Cmd{
		{Args: []string{"pdfinfo", tools.In}, Stdout: "info.txt"},
		{Args: []string{"pdftotext", "-layout", "-enc", "UTF-8", tools.In, tools.Out + "/text.txt"}},
		{Args: []string{"pdfimages", "-list", tools.In}, Stdout: "images.txt"},
	})
	if err != nil {
		return err
	}
	info, err := os.ReadFile(filepath.Join(tmp, "info.txt"))
	if err != nil {
		return err
	}
	pages := pageCount(string(info))
	if pages == 0 {
		return fmt.Errorf("pdfinfo found no pages")
	}
	text, err := os.ReadFile(filepath.Join(tmp, "text.txt"))
	if err != nil {
		return err
	}
	texts := splitPages(string(text), pages)
	list, err := os.ReadFile(filepath.Join(tmp, "images.txt"))
	if err != nil {
		return err
	}
	images := largeImages(string(list), pageArea(string(info)))

	// Second run: an image of each page, and OCR for the pages with little text.
	var cmds []tools.Cmd
	for p := 1; p <= pages; p++ {
		n := strconv.Itoa(p)
		cmds = append(cmds, tools.Cmd{Args: []string{"pdftoppm", "-r", strconv.Itoa(opts.DPI), "-png", "-f", n, "-l", n, "-singlefile", tools.In, tools.Out + "/page-" + n}})
		if letters(texts[p-1]) < opts.MinText {
			f.OCRPages = append(f.OCRPages, p)
			cmds = append(cmds, tools.Cmd{Args: []string{"tesseract", tools.Out + "/page-" + n + ".png", tools.Out + "/page-" + n + ".ocr", "-l", tess}})
		}
	}
	if err := opts.Runner.Run(ctx, input, tmp, cmds); err != nil {
		return err
	}
	f.Pages = pages
	for p := 1; p <= pages; p++ {
		n := strconv.Itoa(p)
		base := path.Join(t.outDir, fmt.Sprintf("page-%03d", p))
		d := casefile.Derived{Source: t.source, From: t.path, Page: p, Images: images[p]}
		if images[p] > 0 {
			f.ImagePages = append(f.ImagePages, p)
		}
		png, err := os.ReadFile(filepath.Join(tmp, "page-"+n+".png"))
		if err != nil {
			return err
		}
		img := d
		img.Kind, img.MediaType = KindPageImage, "image/png"
		if _, err := m.WriteDerived(opts.Root, base+".png", png, img); err != nil {
			return err
		}
		if strings.TrimSpace(texts[p-1]) != "" {
			txt := d
			txt.Kind, txt.MediaType, txt.Method = KindPageText, "text/plain", MethodTextLayer
			if _, err := m.WriteDerived(opts.Root, base+".txt", []byte(texts[p-1]), txt); err != nil {
				return err
			}
		}
		if slices.Contains(f.OCRPages, p) {
			ocr, err := os.ReadFile(filepath.Join(tmp, "page-"+n+".ocr.txt"))
			if err != nil {
				return err
			}
			o := d
			o.Kind, o.MediaType, o.Method, o.Languages = KindPageOCR, "text/plain", MethodOCR, codes
			if _, err := m.WriteDerived(opts.Root, base+".ocr.txt", ocr, o); err != nil {
				return err
			}
		}
	}
	return nil
}

func extractImage(ctx context.Context, opts Options, m *casefile.Manifest, t target, tess string, codes []string) error {
	tmp, err := tempDir(opts.Root)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	input := filepath.Join(opts.Root, filepath.FromSlash(t.path))
	if err := opts.Runner.Run(ctx, input, tmp, []tools.Cmd{
		{Args: []string{"tesseract", tools.In, tools.Out + "/ocr", "-l", tess}},
	}); err != nil {
		return err
	}
	ocr, err := os.ReadFile(filepath.Join(tmp, "ocr.txt"))
	if err != nil {
		return err
	}
	_, err = m.WriteDerived(opts.Root, path.Join(t.outDir, "ocr.txt"), ocr, casefile.Derived{
		Source: t.source, From: t.path, Kind: KindImageOCR, MediaType: "text/plain", Method: MethodOCR, Languages: codes,
	})
	return err
}

// tempDir makes a work folder inside extracted/, so Docker can mount it on
// every OS. It is hidden, so ingest never sees it.
func tempDir(root string) (string, error) {
	dir := filepath.Join(root, casefile.ExtractedDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, ".work-")
}

// pageCount reads "Pages: N" from pdfinfo output.
func pageCount(info string) int {
	for _, line := range strings.Split(info, "\n") {
		if v, ok := strings.CutPrefix(line, "Pages:"); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			return n
		}
	}
	return 0
}

// splitPages splits pdftotext output, which ends each page with a form feed.
func splitPages(text string, pages int) []string {
	parts := strings.Split(text, "\f")
	out := make([]string, pages)
	for i := 0; i < pages && i < len(parts); i++ {
		out[i] = parts[i]
	}
	return out
}

// pageArea reads "Page size: 595.28 x 841.89 pts" from pdfinfo output and
// returns the area in square points. It is 0 when pdfinfo does not show it.
func pageArea(info string) float64 {
	for _, line := range strings.Split(info, "\n") {
		v, ok := strings.CutPrefix(line, "Page size:")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) < 3 || f[1] != "x" {
			return 0
		}
		w, err1 := strconv.ParseFloat(f[0], 64)
		h, err2 := strconv.ParseFloat(f[2], 64)
		if err1 != nil || err2 != nil {
			return 0
		}
		return w * h
	}
	return 0
}

// largeImages counts the images per page in 'pdfimages -list' output that
// cover at least minImageShare of the page. The size on the page comes from
// the pixels and the resolution (x-ppi, y-ppi). When the page area is not
// known, images of at least 200 x 200 pixels count.
func largeImages(list string, area float64) map[int]int {
	counts := map[int]int{}
	header := true
	for _, line := range strings.Split(list, "\n") {
		if header {
			header = !strings.HasPrefix(line, "----")
			continue
		}
		// page num type width height color comp bpc enc interp object ID x-ppi y-ppi size ratio
		f := strings.Fields(line)
		if len(f) < 5 || f[2] != "image" {
			continue
		}
		page, err1 := strconv.Atoi(f[0])
		w, err2 := strconv.ParseFloat(f[3], 64)
		h, err3 := strconv.ParseFloat(f[4], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		large := w*h >= 40000
		if area > 0 && len(f) >= 14 {
			xppi, err4 := strconv.ParseFloat(f[12], 64)
			yppi, err5 := strconv.ParseFloat(f[13], 64)
			if err4 == nil && err5 == nil && xppi > 0 && yppi > 0 {
				large = (w/xppi*72)*(h/yppi*72) >= minImageShare*area
			}
		}
		if large {
			counts[page]++
		}
	}
	return counts
}

// letters counts the letters and digits in s.
func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

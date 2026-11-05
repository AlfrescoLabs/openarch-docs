package email

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
)

// blockTags end a line in the text form of HTML.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "tr": true, "li": true, "table": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "hr": true, "pre": true, "ul": true, "ol": true,
}

// HTMLToText returns the visible text of an HTML document, one block per line.
func HTMLToText(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return cleanText(b.String())
		case html.TextToken:
			if skip == 0 {
				b.WriteString(strings.Join(strings.Fields(string(z.Text())), " "))
				b.WriteByte(' ')
			}
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" || tag == "head" {
				if tt == html.StartTagToken {
					skip++
				} else if tt == html.EndTagToken && skip > 0 {
					skip--
				}
				continue
			}
			if (tag == "td" || tag == "th") && tt == html.StartTagToken {
				b.WriteString("\t")
			}
			if blockTags[tag] {
				b.WriteByte('\n')
			}
		}
	}
}

// cleanText trims each line and keeps at most one empty line in a row.
func cleanText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\u00a0", " "), "\n")
	out := make([]string, 0, len(lines))
	empty := false
	for _, l := range lines {
		var cells []string
		for _, c := range strings.Split(l, "\t") {
			if c = strings.TrimSpace(c); c != "" {
				cells = append(cells, c)
			}
		}
		l = strings.Join(cells, "\t")
		if l == "" {
			if !empty && len(out) > 0 {
				out = append(out, "")
			}
			empty = true
			continue
		}
		out = append(out, l)
		empty = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// forwardMarker matches a line that starts a forwarded or quoted message,
// such as "---------- Forwarded message ---------" or "Begin forwarded message:".
var forwardMarker = regexp.MustCompile(`(?i)^[-_ ]*(` + alternatives(lang.ForwardMarkers()) + `)[-_ ]*:?$`)

// headerKeys maps the header names that mail clients write in quoted messages to MessageInfo fields.
var headerKeys = lang.Headers()

// alternatives joins words into a regular expression alternation.
func alternatives(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = regexp.QuoteMeta(w)
	}
	return strings.Join(q, "|")
}

// Segment is a forwarded or quoted message in a body text.
type Segment struct {
	// Start is the byte offset in the text, with "\r\n" turned into "\n",
	// where the message starts: its marker or separator line, or its first
	// header line.
	Start int
	Info  casefile.MessageInfo
}

// separator matches a line of dashes or underscores, as Gmail writes above
// each message of a forwarded conversation.
var separator = regexp.MustCompile(`^[-_]{5,}$`)

// replyTime matches the time that ends the date of a reply line: "9:10 AM", "18:00".
var replyTime = regexp.MustCompile(`\d{1,2}[:h]\d{2}(?::\d{2})?(?:\s?[AaPp]\.?\s?[Mm]\.?)?`)

// splitReply splits the middle of a reply line into the date and the sender.
// The date ends with its time; without a time, at the last comma.
func splitReply(s string) (date, from string) {
	if loc := replyTime.FindAllStringIndex(s, -1); loc != nil {
		end := loc[len(loc)-1][1]
		return strings.TrimSpace(s[:end]), strings.TrimSpace(strings.TrimLeft(s[end:], ", "))
	}
	if i := strings.LastIndex(s, ","); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "", strings.TrimSpace(s)
}

// Segments finds the forwarded and quoted messages in a body text, in order:
// a marker line followed by header lines, a "From:" line (in any supported
// language) followed by a date or subject line, or a reply line such as
// "On <date>, <sender> wrote:".
func Segments(text string) []Segment {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	offsets := make([]int, len(lines))
	for i, off := 0, 0; i < len(lines); i++ {
		offsets[i] = off
		off += len(lines[i]) + 1
	}
	var found []Segment
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(strings.TrimLeft(lines[i], "> "))
		if m := lang.ReplyRE.FindStringSubmatch(line); m != nil {
			var info casefile.MessageInfo
			info.Date, info.From = splitReply(m[1])
			found = append(found, Segment{Start: offsets[i], Info: info})
			continue
		}
		start := i
		if forwardMarker.MatchString(line) {
			start = i + 1
		} else if key, _, ok := headerLine(line); !ok || key != "from" {
			continue
		}
		info, n := readHeaders(lines[start:])
		if info.From == "" || (info.Subject == "" && info.Date == "") {
			continue
		}
		first := i
		if first == start && first > 0 && separator.MatchString(strings.TrimSpace(lines[first-1])) {
			first--
		}
		found = append(found, Segment{Start: offsets[first], Info: info})
		i = start + n - 1
	}
	return found
}

// Forwarded returns the headers of the forwarded and quoted messages in a
// body text, in order. See Segments.
func Forwarded(text string) []casefile.MessageInfo {
	var out []casefile.MessageInfo
	for _, s := range Segments(text) {
		out = append(out, s.Info)
	}
	return out
}

// readHeaders reads header lines until the first line that is not a header.
// It returns the headers and the number of lines read.
func readHeaders(lines []string) (casefile.MessageInfo, int) {
	var info casefile.MessageInfo
	n := 0
	for ; n < len(lines); n++ {
		line := strings.TrimSpace(strings.TrimLeft(lines[n], "> "))
		key, value, ok := headerLine(line)
		if !ok {
			break
		}
		switch key {
		case "from":
			if info.From != "" {
				return info, n
			}
			info.From = value
		case "date":
			info.Date = value
		case "subject":
			info.Subject = value
		case "to":
			info.To = value
		case "cc":
			info.Cc = value
		}
	}
	return info, n
}

func headerLine(line string) (key, value string, ok bool) {
	k, v, found := strings.Cut(line, ":")
	if !found {
		return "", "", false
	}
	key, ok = headerKeys[strings.ToLower(strings.Trim(strings.TrimSpace(k), "*"))]
	return key, strings.TrimSpace(strings.Trim(strings.TrimSpace(v), "*")), ok
}

// cp1252 maps the C1 control characters U+0080 to U+009F to the Windows-1252
// characters with the same byte. Many emails say ISO-8859-1 but use
// Windows-1252, so a byte 0x96 (en dash) decodes as U+0096. Browsers read
// ISO-8859-1 as Windows-1252 for the same reason.
var cp1252 = map[rune]rune{
	0x80: '\u20ac', 0x82: '\u201a', 0x83: '\u0192', 0x84: '\u201e', 0x85: '\u2026', 0x86: '\u2020', 0x87: '\u2021',
	0x88: '\u02c6', 0x89: '\u2030', 0x8a: '\u0160', 0x8b: '\u2039', 0x8c: '\u0152', 0x8e: '\u017d',
	0x91: '\u2018', 0x92: '\u2019', 0x93: '\u201c', 0x94: '\u201d', 0x95: '\u2022', 0x96: '\u2013', 0x97: '\u2014',
	0x98: '\u02dc', 0x99: '\u2122', 0x9a: '\u0161', 0x9b: '\u203a', 0x9c: '\u0153', 0x9e: '\u017e', 0x9f: '\u0178',
}

// fixC1 replaces C1 control characters with their Windows-1252 characters.
func fixC1(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r >= 0x80 && r <= 0x9f }) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if c, ok := cp1252[r]; ok {
			return c
		}
		return r
	}, s)
}

// entity matches the HTML entities that some mail clients leave in plain text parts.
var entity = regexp.MustCompile(`&(lt|gt|amp|quot|apos|nbsp|#[0-9]{1,6}|#x[0-9a-fA-F]{1,6});`)

// unescapePlain decodes HTML entities in a plain text part, such as
// "&lt;ana@example.com&gt;" written by some mail clients.
func unescapePlain(s string) string {
	if !entity.MatchString(s) {
		return s
	}
	return html.UnescapeString(s)
}

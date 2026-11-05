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

// Forwarded finds the forwarded or quoted messages in a body text: a marker
// line followed by header lines, or a "From:" line (in any supported
// language) followed by a date or subject line.
func Forwarded(text string) []casefile.MessageInfo {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var found []casefile.MessageInfo
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(strings.TrimLeft(lines[i], "> "))
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
		found = append(found, info)
		i = start + n - 1
	}
	return found
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

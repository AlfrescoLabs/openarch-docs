// Package timeline lists the dated events of a case folder: emails, the
// forwarded messages quoted in their bodies, and chat messages, in time order.
// It needs no model.
package timeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
	"github.com/AlfrescoLabs/openarch-docs/internal/chat"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
	"github.com/AlfrescoLabs/openarch-docs/internal/lang"
)

// Kinds of an event.
const (
	KindEmail     = "email"
	KindForwarded = "forwarded"
	KindChat      = "chat"
)

// Event is one dated event.
type Event struct {
	// Time is "2006-01-02T15:04" in the time zone the source used, or empty
	// when the date could not be read.
	Time string `json:"time"`
	// Raw is the date as the source wrote it.
	Raw     string `json:"raw"`
	Kind    string `json:"kind"`
	From    string `json:"from"`
	To      string `json:"to,omitempty"`
	Subject string `json:"subject,omitempty"`
	Text    string `json:"text,omitempty"`
	// Source is the original, and Path the derived file the event is in.
	Source string `json:"source"`
	Path   string `json:"path"`
}

// Options filter the events.
type Options struct {
	Root string
	// Since and Until keep events in a range of "2006-01-02" dates, both included.
	Since, Until string
	// Kinds keeps only these kinds. Empty keeps all.
	Kinds []string
	// Undated keeps events whose date could not be read; they come last.
	Undated bool
}

// maxText is the length of chat texts in an event.
const maxText = 300

// Build returns the events of the case folder, oldest first.
func Build(opts Options) ([]Event, error) {
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	var events []Event
	// The same message can be an email in one file and a quoted forward in
	// another. Keep one event, and prefer the email, which has all headers.
	seen := map[string]int{}
	add := func(e Event) {
		var key string
		switch {
		case e.Kind == KindChat:
			key = "chat|" + e.Time + "|" + e.From + "|" + e.Text
		case e.Time != "":
			key = "mail|" + e.Time + "|" + address(e.From)
		default:
			key = "raw|" + e.Raw + "|" + address(e.From) + "|" + e.Subject
		}
		if i, ok := seen[key]; ok {
			if events[i].Kind == KindForwarded && e.Kind == KindEmail {
				events[i] = e
			}
			return
		}
		seen[key] = len(events)
		events = append(events, e)
	}
	for _, d := range m.Derived {
		switch d.Kind {
		case ingest.KindBody:
			if d.Message != nil {
				add(Event{Time: ParseDate(d.Message.Date), Raw: d.Message.Date, Kind: KindEmail, From: d.Message.From, To: d.Message.To,
					Subject: d.Message.Subject, Source: d.Source, Path: d.Path})
			}
			for _, f := range d.Forwarded {
				add(Event{Time: ParseDate(f.Date), Raw: f.Date, Kind: KindForwarded, From: f.From, To: f.To,
					Subject: f.Subject, Source: d.Source, Path: d.Path})
			}
		case ingest.KindChatJSON:
			data, err := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(d.Path)))
			if err != nil {
				return nil, err
			}
			var msgs []chat.Message
			if err := json.Unmarshal(data, &msgs); err != nil {
				return nil, fmt.Errorf("%s: %w", d.Path, err)
			}
			for _, c := range msgs {
				t := ""
				if len(c.ISO) >= 16 {
					t = c.ISO[:16]
				}
				text := c.Text
				if c.Attachment != "" {
					text = "[" + path.Base(c.Attachment) + "]"
				}
				if r := []rune(text); len(r) > maxText {
					text = string(r[:maxText]) + "..."
				}
				add(Event{Time: t, Raw: c.Time, Kind: KindChat, From: c.Sender, Text: text, Source: d.Source, Path: d.Path})
			}
		}
	}
	events = slices.DeleteFunc(events, func(e Event) bool {
		switch {
		case len(opts.Kinds) > 0 && !slices.Contains(opts.Kinds, e.Kind):
			return true
		case e.Time == "":
			return !opts.Undated
		case opts.Since != "" && e.Time[:10] < opts.Since:
			return true
		case opts.Until != "" && e.Time[:10] > opts.Until:
			return true
		}
		return false
	})
	slices.SortStableFunc(events, func(a, b Event) int {
		switch {
		case a.Time == "" && b.Time != "":
			return 1
		case a.Time != "" && b.Time == "":
			return -1
		}
		return strings.Compare(a.Time, b.Time)
	})
	return events, nil
}

// address returns the email address in a From value, in lower case, or the value.
func address(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			return strings.ToLower(from[i+1 : i+j])
		}
	}
	return strings.ToLower(strings.TrimSpace(from))
}

var (
	months     = lang.MonthNumbers()
	clockRE    = regexp.MustCompile(`(\d{1,2})[:h](\d{2})(?::(\d{2}))?\s*(a\.?\s?m\.?|p\.?\s?m\.?)?`)
	numericRE  = regexp.MustCompile(`\b(\d{1,4})[/.-](\d{1,2})[/.-](\d{2,4})\b`)
	wordRE     = regexp.MustCompile(`[\pL]+\.?|\d+`)
	yearRE     = regexp.MustCompile(`^(19|20)\d\d$`)
	timeLayout = "2006-01-02T15:04"
)

// ParseDate reads a date as mail clients write it, in any supported language:
// RFC 3339 from email headers, "Fri, Oct 30, 2026 at 5:51 PM",
// "mie, 29 oct 2026 a las 18:00", "29 de outubro de 2026 18:00" or
// "30/10/2026 7:51". It returns "2006-01-02T15:04", or "" when it cannot.
func ParseDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Format(timeLayout)
	}
	// Gmail writes "5:51\u202fPM" with a narrow no-break space; Go regexp \s
	// is ASCII only, so every Unicode space becomes a plain space.
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case r == '\u200b' || r == '\u200e' || r == '\u200f':
			return -1
		}
		return r
	}, s)
	low := strings.ToLower(fold(s))
	hour, minute := 0, 0
	if c := clockRE.FindStringSubmatchIndex(low); c != nil {
		hour, _ = strconv.Atoi(low[c[2]:c[3]])
		minute, _ = strconv.Atoi(low[c[4]:c[5]])
		if c[8] >= 0 {
			ampm := strings.NewReplacer(".", "", " ", "").Replace(low[c[8]:c[9]])
			if ampm == "pm" && hour < 12 {
				hour += 12
			}
			if ampm == "am" && hour == 12 {
				hour = 0
			}
		}
		// Remove the time, so its numbers are not read as a day.
		low = low[:c[0]] + " " + low[c[1]:]
	}
	year, month, day := 0, 0, 0
	if n := numericRE.FindStringSubmatch(low); n != nil {
		a, _ := strconv.Atoi(n[1])
		b, _ := strconv.Atoi(n[2])
		c, _ := strconv.Atoi(n[3])
		switch {
		case len(n[1]) == 4: // 2026-10-30
			year, month, day = a, b, c
		default: // day first, as in Spanish, French and Portuguese, unless the month would be above 12
			day, month, year = a, b, c
			if month > 12 && day <= 12 {
				day, month = month, day
			}
		}
	} else {
		var numbers []int
		for _, w := range wordRE.FindAllString(low, -1) {
			w = strings.TrimSuffix(w, ".")
			if mo, ok := months[w]; ok && month == 0 {
				month = mo
				continue
			}
			if v, err := strconv.Atoi(w); err == nil {
				if yearRE.MatchString(w) && year == 0 {
					year = v
				} else {
					numbers = append(numbers, v)
				}
			}
		}
		for _, v := range numbers {
			if v >= 1 && v <= 31 {
				day = v
				break
			}
		}
	}
	if year > 0 && year < 100 {
		year += 2000
	}
	t := time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
	if year < 1900 || month < 1 || month > 12 || day < 1 || t.Day() != day || hour > 23 || minute > 59 {
		return ""
	}
	return t.Format(timeLayout)
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

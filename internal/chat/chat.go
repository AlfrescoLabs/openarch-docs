// Package chat reads WhatsApp chat exports: the zip with the chat text file
// and the media files.
package chat

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Message is one chat message.
type Message struct {
	// Time is the date and time as written in the export.
	Time string `json:"time"`
	// ISO is Time as "2006-01-02T15:04:05", when the date order is known.
	ISO        string `json:"iso,omitempty"`
	Sender     string `json:"sender"`
	Text       string `json:"text"`
	Attachment string `json:"attachment,omitempty"`
}

var (
	// iOS: [24/8/26, 18:05:36] Name: text
	iosLine = regexp.MustCompile(`^\[(\d{1,2})/(\d{1,2})/(\d{2,4}),? (\d{1,2}):(\d{2})(?::(\d{2}))?\s?([AaPp]\.?\s?[Mm]\.?)?\] ([^:]+): (.*)$`)
	// Android: 24/8/26, 18:05 - Name: text
	androidLine = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{2,4}),? (\d{1,2}):(\d{2})(?::(\d{2}))?\s?([AaPp]\.?\s?[Mm]\.?)?\s[-\x{2013}] ([^:]+): (.*)$`)
	// <attached: 00000023-PHOTO-2026-09-10-18-10-04.jpg> and the Spanish form <adjunto: ...>
	iosAttachment = regexp.MustCompile(`^<(?:attached|adjunto): (.+)>$`)
	// IMG-20260910-WA0001.jpg (file attached) and the Spanish form (archivo adjunto)
	androidAttachment = regexp.MustCompile(`^(.+\.[A-Za-z0-9]{2,5}) \((?:file attached|archivo adjunto)\)`)
)

// IsChatFile reports whether a file name in a zip is a WhatsApp chat text file.
func IsChatFile(name string) bool {
	base := path.Base(name)
	return base == "_chat.txt" || (strings.HasPrefix(base, "WhatsApp Chat") && strings.HasSuffix(base, ".txt"))
}

// Parse reads the chat text. Lines that do not start a message continue the previous one.
func Parse(text string) []Message {
	text = strings.NewReplacer("\u200e", "", "\u200f", "", "\u202f", " ", "\u00a0", " ", "\r\n", "\n").Replace(text)
	text = strings.TrimPrefix(text, "\ufeff")
	type raw struct {
		day, month, year, hour, minute, second, ampm string
		msg                                          Message
	}
	var msgs []raw
	for _, line := range strings.Split(text, "\n") {
		g := iosLine.FindStringSubmatch(line)
		if g == nil {
			g = androidLine.FindStringSubmatch(line)
		}
		if g == nil {
			if len(msgs) > 0 && line != "" {
				last := &msgs[len(msgs)-1].msg
				last.Text += "\n" + line
			}
			continue
		}
		body := strings.TrimSpace(g[9])
		m := Message{Time: timeText(line), Sender: strings.TrimSpace(g[8]), Text: body}
		if a := iosAttachment.FindStringSubmatch(body); a != nil {
			m.Attachment = a[1]
		} else if a := androidAttachment.FindStringSubmatch(body); a != nil {
			m.Attachment = a[1]
		}
		msgs = append(msgs, raw{g[1], g[2], g[3], g[4], g[5], g[6], g[7], m})
	}
	// The export writes the date in the order of the phone locale. A first
	// number above 12 means day first; a second number above 12 means month first.
	dayFirst, known := true, false
	for _, r := range msgs {
		a, _ := strconv.Atoi(r.day)
		b, _ := strconv.Atoi(r.month)
		if a > 12 {
			dayFirst, known = true, true
			break
		}
		if b > 12 {
			dayFirst, known = false, true
			break
		}
	}
	out := make([]Message, len(msgs))
	for i, r := range msgs {
		out[i] = r.msg
		if !known {
			continue
		}
		day, month := r.day, r.month
		if !dayFirst {
			day, month = month, day
		}
		out[i].ISO = iso(r.year, month, day, r.hour, r.minute, r.second, r.ampm)
	}
	return out
}

// timeText returns the date and time part of a message line, as written.
func timeText(line string) string {
	if strings.HasPrefix(line, "[") {
		if end := strings.Index(line, "]"); end > 0 {
			return line[1:end]
		}
	}
	if i := strings.Index(line, " - "); i > 0 {
		return line[:i]
	}
	return ""
}

func iso(year, month, day, hour, minute, second, ampm string) string {
	y, _ := strconv.Atoi(year)
	if y < 100 {
		y += 2000
	}
	mo, _ := strconv.Atoi(month)
	d, _ := strconv.Atoi(day)
	h, _ := strconv.Atoi(hour)
	mi, _ := strconv.Atoi(minute)
	s, _ := strconv.Atoi(second)
	switch strings.ToLower(strings.NewReplacer(".", "", " ", "").Replace(ampm)) {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d", y, mo, d, h, mi, s)
}

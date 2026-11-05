// Package email parses .eml files: headers, body text, attachments, inline
// images and nested messages (message/rfc822 parts).
package email

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-message"
	// Registers the character sets that go-message decodes, such as ISO-8859-1.
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
)

// maxDepth limits nested messages, so a crafted file cannot recurse forever.
const maxDepth = 20

// Message is one parsed email.
type Message struct {
	Info casefile.MessageInfo
	// Text is the plain text body. When the email has only HTML, it is the HTML as text.
	Text string
	// HTML is the HTML body, if any.
	HTML string
	// Parts are the attachments and inline images.
	Parts []Part
	// Nested are the emails attached to this one.
	Nested []Nested
}

// Part is an attachment or an inline image.
type Part struct {
	// Number is the MIME part number, such as "2.1".
	Number    string
	MediaType string
	Filename  string
	Inline    bool
	Data      []byte
}

// Nested is an email attached to another one.
type Nested struct {
	Number  string
	Raw     []byte
	Message *Message
}

// Parse reads an email.
func Parse(r io.Reader) (*Message, error) {
	return parse(r, 0)
}

func parse(r io.Reader, depth int) (*Message, error) {
	if depth > maxDepth {
		return nil, errors.New("too many nested messages")
	}
	e, err := message.Read(r)
	if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
		return nil, err
	}
	m := &Message{Info: headerInfo(mail.Header{Header: e.Header})}
	var text, html []string
	if err := walk(e, "", depth, m, &text, &html); err != nil {
		return nil, err
	}
	m.Text = fixC1(strings.TrimSpace(strings.Join(text, "\n\n")))
	m.HTML = fixC1(strings.Join(html, "\n"))
	if m.Text == "" && m.HTML != "" {
		m.Text = HTMLToText(m.HTML)
	}
	return m, nil
}

func walk(e *message.Entity, number string, depth int, m *Message, text, html *[]string) error {
	if mr := e.MultipartReader(); mr != nil {
		alternative := mediaType(e.Header) == "multipart/alternative"
		var altText, altHTML string
		var childText []string
		for i := 1; ; i++ {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
				return err
			}
			n := strconv.Itoa(i)
			if number != "" {
				n = number + "." + n
			}
			if alternative && !isAttachment(p.Header) {
				switch mediaType(p.Header) {
				case "text/plain":
					b, err := io.ReadAll(p.Body)
					if err != nil {
						return err
					}
					altText = unescapePlain(string(b))
					continue
				case "text/html":
					b, err := io.ReadAll(p.Body)
					if err != nil {
						return err
					}
					altHTML = string(b)
					continue
				}
			}
			if !alternative {
				if err := walk(p, n, depth, m, text, html); err != nil {
					return err
				}
				continue
			}
			if err := walk(p, n, depth, m, &childText, html); err != nil {
				return err
			}
		}
		if !alternative {
			return nil
		}
		// The children of multipart/alternative are forms of the same body.
		// Keep the plain text form when there is one.
		switch {
		case altText != "":
			*text = append(*text, altText)
		case altHTML != "":
			*text = append(*text, HTMLToText(altHTML))
		default:
			*text = append(*text, childText...)
		}
		if altHTML != "" {
			*html = append(*html, altHTML)
		}
		return nil
	}
	if number == "" {
		number = "1"
	}
	mt := mediaType(e.Header)
	data, err := io.ReadAll(e.Body)
	if err != nil {
		return fmt.Errorf("part %s: %w", number, err)
	}
	switch {
	case mt == "message/rfc822":
		nested, err := parse(bytes.NewReader(data), depth+1)
		if err != nil {
			return fmt.Errorf("part %s: %w", number, err)
		}
		m.Nested = append(m.Nested, Nested{Number: number, Raw: data, Message: nested})
	case mt == "text/plain" && !isAttachment(e.Header):
		*text = append(*text, unescapePlain(string(data)))
	case mt == "text/html" && !isAttachment(e.Header):
		*html = append(*html, string(data))
		*text = append(*text, HTMLToText(string(data)))
	default:
		ah := mail.AttachmentHeader{Header: e.Header}
		filename, _ := ah.Filename()
		if filename == "" {
			_, params, _ := e.Header.ContentType()
			filename = params["name"]
		}
		disp, _, _ := e.Header.ContentDisposition()
		m.Parts = append(m.Parts, Part{
			Number:    number,
			MediaType: mt,
			Filename:  fixC1(filename),
			Inline:    disp == "inline" || (disp == "" && e.Header.Get("Content-Id") != ""),
			Data:      data,
		})
	}
	return nil
}

func mediaType(h message.Header) string {
	mt, _, err := h.ContentType()
	if err != nil || mt == "" {
		return "text/plain"
	}
	return strings.ToLower(mt)
}

func isAttachment(h message.Header) bool {
	disp, _, _ := h.ContentDisposition()
	return disp == "attachment"
}

func headerInfo(h mail.Header) casefile.MessageInfo {
	info := casefile.MessageInfo{
		From:      addresses(h, "From"),
		To:        addresses(h, "To"),
		Cc:        addresses(h, "Cc"),
		MessageID: strings.Trim(h.Get("Message-Id"), "<> "),
	}
	info.Subject, _ = h.Subject()
	if d, err := h.Date(); err == nil && !d.IsZero() {
		info.Date = d.Format(time.RFC3339)
	} else {
		info.Date = h.Get("Date")
	}
	for _, f := range []*string{&info.From, &info.To, &info.Cc, &info.Subject} {
		*f = fixC1(*f)
	}
	return info
}

// addresses returns an address header as text. When the list does not parse,
// it returns the header with encoded words decoded.
func addresses(h mail.Header, key string) string {
	list, err := h.AddressList(key)
	if err != nil || len(list) == 0 {
		raw := h.Get(key)
		dec := mime.WordDecoder{}
		if s, err := dec.DecodeHeader(raw); err == nil {
			return s
		}
		return raw
	}
	parts := make([]string, len(list))
	for i, a := range list {
		if a.Name != "" {
			parts[i] = fmt.Sprintf("%s <%s>", a.Name, a.Address)
		} else {
			parts[i] = a.Address
		}
	}
	return strings.Join(parts, ", ")
}

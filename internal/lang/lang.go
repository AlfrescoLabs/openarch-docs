// Package lang holds the words that mail clients, chat exports and OCR use in
// each supported language. Parsers match every language at once, so a case
// folder can mix languages. To add a language, add one entry to All.
package lang

import (
	"fmt"
	"slices"
	"strings"
)

// Language is the data for one language.
type Language struct {
	// Code is the ISO 639-1 code, such as "es".
	Code string
	Name string
	// Tesseract is the tesseract language code, such as "spa".
	Tesseract string
	// ForwardMarkers are the words of the line that starts a forwarded or
	// quoted message, such as "Forwarded message" or "Begin forwarded message".
	ForwardMarkers []string
	// Headers maps the header names that mail clients write in quoted messages
	// to "from", "date", "to", "cc" or "subject".
	Headers map[string]string
	// ChatAttached are the words of a WhatsApp iOS attachment line:
	// "<attached: 00000023-PHOTO-....jpg>".
	ChatAttached []string
	// ChatFileAttached are the words of a WhatsApp Android attachment line:
	// "IMG-....jpg (file attached)".
	ChatFileAttached []string
}

// All lists the supported languages.
var All = []Language{
	{
		Code: "en", Name: "English", Tesseract: "eng",
		ForwardMarkers: []string{"Forwarded message", "Original message", "Begin forwarded message"},
		Headers: map[string]string{
			"from": "from", "sent": "date", "date": "date", "to": "to", "cc": "cc", "subject": "subject",
		},
		ChatAttached:     []string{"attached"},
		ChatFileAttached: []string{"file attached"},
	},
	{
		Code: "es", Name: "Spanish", Tesseract: "spa",
		ForwardMarkers: []string{"Mensaje reenviado", "Mensaje original", "Inicio del mensaje reenviado"},
		Headers: map[string]string{
			"de": "from", "enviado": "date", "enviado el": "date", "fecha": "date", "para": "to", "cc": "cc", "asunto": "subject",
		},
		ChatAttached:     []string{"adjunto"},
		ChatFileAttached: []string{"archivo adjunto"},
	},
	{
		Code: "fr", Name: "French", Tesseract: "fra",
		ForwardMarkers: []string{"Message transféré", "Message d'origine", "Message original", "Début du message réexpédié", "Début du message transféré"},
		Headers: map[string]string{
			"de": "from", "envoyé": "date", "envoyé le": "date", "date": "date", "à": "to", "a": "to", "cc": "cc", "objet": "subject", "sujet": "subject",
		},
		ChatAttached:     []string{"pièce jointe"},
		ChatFileAttached: []string{"fichier joint"},
	},
	{
		Code: "pt", Name: "Portuguese", Tesseract: "por",
		ForwardMarkers: []string{"Mensagem encaminhada", "Mensagem reencaminhada", "Mensagem original", "Início da mensagem encaminhada", "Início da mensagem reencaminhada"},
		Headers: map[string]string{
			"de": "from", "enviado": "date", "enviada": "date", "enviado em": "date", "data": "date", "para": "to", "cc": "cc", "assunto": "subject",
		},
		ChatAttached:     []string{"anexado", "anexo"},
		ChatFileAttached: []string{"arquivo anexado", "ficheiro anexado"},
	},
}

// Codes returns the codes of every supported language.
func Codes() []string {
	codes := make([]string, len(All))
	for i, l := range All {
		codes[i] = l.Code
	}
	return codes
}

// Get returns the languages with the given codes, in that order.
func Get(codes []string) ([]Language, error) {
	out := make([]Language, 0, len(codes))
	for _, c := range codes {
		c = strings.ToLower(strings.TrimSpace(c))
		i := slices.IndexFunc(All, func(l Language) bool { return l.Code == c })
		if i < 0 {
			return nil, fmt.Errorf("unknown language %q; supported: %s", c, strings.Join(Codes(), ", "))
		}
		out = append(out, All[i])
	}
	return out, nil
}

// ForwardMarkers returns the forward markers of every language.
func ForwardMarkers() []string {
	var out []string
	for _, l := range All {
		out = append(out, l.ForwardMarkers...)
	}
	return out
}

// Headers returns the quoted header names of every language. A name used by
// two languages maps to the same field in both.
func Headers() map[string]string {
	out := map[string]string{}
	for _, l := range All {
		for k, v := range l.Headers {
			out[k] = v
		}
	}
	return out
}

// ChatAttached returns the iOS attachment words of every language.
func ChatAttached() []string {
	var out []string
	for _, l := range All {
		out = append(out, l.ChatAttached...)
	}
	return out
}

// ChatFileAttached returns the Android attachment words of every language.
func ChatFileAttached() []string {
	var out []string
	for _, l := range All {
		out = append(out, l.ChatFileAttached...)
	}
	return out
}

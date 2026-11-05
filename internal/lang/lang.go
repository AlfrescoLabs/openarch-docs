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
	// StopWords are short common words that full text search ignores.
	StopWords []string
	// Months are the month names and short forms, without accents, from
	// January to December. Mail clients write them in quoted dates.
	Months [12][]string
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
		StopWords:        []string{"the", "a", "an", "and", "or", "of", "to", "in", "on", "at", "by", "for", "with", "from", "is", "are", "was", "were", "be", "been", "it", "its", "this", "that", "these", "those", "as", "not", "no", "but", "if", "so", "do", "does", "did", "has", "have", "had", "i", "you", "he", "she", "we", "they", "me", "my", "your", "our", "their"},
		Months:           [12][]string{{"january", "jan"}, {"february", "feb"}, {"march", "mar"}, {"april", "apr"}, {"may"}, {"june", "jun"}, {"july", "jul"}, {"august", "aug"}, {"september", "sep", "sept"}, {"october", "oct"}, {"november", "nov"}, {"december", "dec"}},
	},
	{
		Code: "es", Name: "Spanish", Tesseract: "spa",
		ForwardMarkers: []string{"Mensaje reenviado", "Mensaje original", "Inicio del mensaje reenviado"},
		Headers: map[string]string{
			"de": "from", "enviado": "date", "enviado el": "date", "fecha": "date", "para": "to", "cc": "cc", "asunto": "subject",
		},
		ChatAttached:     []string{"adjunto"},
		ChatFileAttached: []string{"archivo adjunto"},
		StopWords:        []string{"el", "la", "los", "las", "un", "una", "unos", "unas", "y", "o", "u", "de", "del", "al", "a", "en", "con", "por", "para", "sin", "sobre", "es", "son", "era", "fue", "ser", "estar", "esta", "este", "esto", "estos", "estas", "ese", "esa", "eso", "lo", "le", "les", "se", "su", "sus", "mi", "mis", "tu", "tus", "que", "muy", "mas", "pero", "si", "no", "ya", "hay", "me", "te", "nos", "os", "ha", "han", "he"},
		Months:           [12][]string{{"enero", "ene"}, {"febrero", "feb"}, {"marzo", "mar"}, {"abril", "abr"}, {"mayo", "may"}, {"junio", "jun"}, {"julio", "jul"}, {"agosto", "ago"}, {"septiembre", "setiembre", "sep", "sept"}, {"octubre", "oct"}, {"noviembre", "nov"}, {"diciembre", "dic"}},
	},
	{
		Code: "fr", Name: "French", Tesseract: "fra",
		ForwardMarkers: []string{"Message transféré", "Message d'origine", "Message original", "Début du message réexpédié", "Début du message transféré"},
		Headers: map[string]string{
			"de": "from", "envoyé": "date", "envoyé le": "date", "date": "date", "à": "to", "a": "to", "cc": "cc", "objet": "subject", "sujet": "subject",
		},
		ChatAttached:     []string{"pièce jointe"},
		ChatFileAttached: []string{"fichier joint"},
		StopWords:        []string{"le", "la", "les", "un", "une", "des", "et", "ou", "de", "du", "au", "aux", "a", "en", "dans", "sur", "par", "pour", "avec", "sans", "est", "sont", "etait", "etre", "ce", "cet", "cette", "ces", "se", "sa", "son", "ses", "mon", "ma", "mes", "ton", "ta", "tes", "que", "qui", "ou", "ne", "pas", "plus", "mais", "si", "il", "elle", "ils", "elles", "nous", "vous", "je", "tu", "on", "y"},
		Months:           [12][]string{{"janvier", "janv"}, {"fevrier", "fevr", "fev"}, {"mars"}, {"avril", "avr"}, {"mai"}, {"juin"}, {"juillet", "juil"}, {"aout"}, {"septembre", "sept"}, {"octobre", "oct"}, {"novembre", "nov"}, {"decembre", "dec"}}, //nolint:misspell // French "decembre" is "décembre" without the accent.
	},
	{
		Code: "pt", Name: "Portuguese", Tesseract: "por",
		ForwardMarkers: []string{"Mensagem encaminhada", "Mensagem reencaminhada", "Mensagem original", "Início da mensagem encaminhada", "Início da mensagem reencaminhada"},
		Headers: map[string]string{
			"de": "from", "enviado": "date", "enviada": "date", "enviado em": "date", "data": "date", "para": "to", "cc": "cc", "assunto": "subject",
		},
		ChatAttached:     []string{"anexado", "anexo"},
		ChatFileAttached: []string{"arquivo anexado", "ficheiro anexado"},
		StopWords:        []string{"o", "a", "os", "as", "um", "uma", "uns", "umas", "e", "ou", "de", "do", "da", "dos", "das", "ao", "aos", "em", "no", "na", "nos", "nas", "por", "para", "com", "sem", "sobre", "e", "sao", "era", "foi", "ser", "estar", "este", "esta", "isto", "esse", "essa", "isso", "se", "seu", "sua", "seus", "suas", "meu", "minha", "que", "muito", "mais", "mas", "sim", "nao", "ja", "ha", "me", "te", "lhe"},
		Months:           [12][]string{{"janeiro", "jan"}, {"fevereiro", "fev"}, {"marco", "mar"}, {"abril", "abr"}, {"maio", "mai"}, {"junho", "jun"}, {"julho", "jul"}, {"agosto", "ago"}, {"setembro", "set"}, {"outubro", "out"}, {"novembro", "nov"}, {"dezembro", "dez"}},
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

// AllStopWords returns the stop words of every language, without accents.
func AllStopWords() map[string]bool {
	out := map[string]bool{}
	for _, l := range All {
		for _, w := range l.StopWords {
			out[w] = true
		}
	}
	return out
}

// MonthNumbers maps every month name and short form of every language,
// without accents, to its number from 1 to 12.
func MonthNumbers() map[string]int {
	out := map[string]int{}
	for _, l := range All {
		for i, names := range l.Months {
			for _, n := range names {
				out[n] = i + 1
			}
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

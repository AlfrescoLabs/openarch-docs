// Package lang holds the words that mail clients, chat exports and OCR use in
// each supported language. Parsers match every language at once, so a case
// folder can mix languages. To add a language, add one entry to All.
package lang

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
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
	// Reply is the first word and the verb of the line that mail clients
	// write above a quoted reply: "On <date>, <sender> wrote:".
	Reply [2]string
	// LatestWords ask for the newest documents, such as "last" or "latest",
	// without accents.
	LatestWords []string
	// StatePhrases ask how the case stands now, such as "next steps" or
	// "current state", without accents. The newest documents answer them.
	StatePhrases []string
	// StopWords are short common words that full text search ignores.
	StopWords []string
	// PageWords are the words and short forms for "page", without accents.
	PageWords []string
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
		Reply:            [2]string{"On", "wrote"},
		LatestWords:      []string{"last", "latest", "newest", "recent"},
		StatePhrases:     []string{"next steps", "next step", "current state", "current status", "status of the case", "state of the case"},
		StopWords:        []string{"the", "a", "an", "and", "or", "of", "to", "in", "on", "at", "by", "for", "with", "from", "is", "are", "was", "were", "be", "been", "it", "its", "this", "that", "these", "those", "as", "not", "no", "but", "if", "so", "do", "does", "did", "has", "have", "had", "i", "you", "he", "she", "we", "they", "me", "my", "your", "our", "their"},
		PageWords:        []string{"page", "pages", "pg", "p"},
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
		Reply:            [2]string{"El", "escribió"},
		LatestWords:      []string{"ultimo", "ultima", "ultimos", "ultimas", "reciente", "recientes"},
		StatePhrases:     []string{"siguientes pasos", "siguiente paso", "proximos pasos", "proximo paso", "estado actual", "estado del caso", "estado del expediente"},
		StopWords:        []string{"el", "la", "los", "las", "un", "una", "unos", "unas", "y", "o", "u", "de", "del", "al", "a", "en", "con", "por", "para", "sin", "sobre", "es", "son", "era", "fue", "ser", "estar", "esta", "este", "esto", "estos", "estas", "ese", "esa", "eso", "lo", "le", "les", "se", "su", "sus", "mi", "mis", "tu", "tus", "que", "muy", "mas", "pero", "si", "no", "ya", "hay", "me", "te", "nos", "os", "ha", "han", "he"},
		PageWords:        []string{"pagina", "paginas", "pag", "p"},
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
		Reply:            [2]string{"Le", "a écrit"},
		LatestWords:      []string{"dernier", "derniere", "derniers", "recent", "recente"},
		StatePhrases:     []string{"prochaines etapes", "prochaine etape", "etat actuel", "etat du dossier", "ou en est"},
		StopWords:        []string{"le", "la", "les", "un", "une", "des", "et", "ou", "de", "du", "au", "aux", "a", "en", "dans", "sur", "par", "pour", "avec", "sans", "est", "sont", "etait", "etre", "ce", "cet", "cette", "ces", "se", "sa", "son", "ses", "mon", "ma", "mes", "ton", "ta", "tes", "que", "qui", "ou", "ne", "pas", "plus", "mais", "si", "il", "elle", "ils", "elles", "nous", "vous", "je", "tu", "on", "y"},
		PageWords:        []string{"page", "pages", "p"},
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
		Reply:            [2]string{"Em", "escreveu"},
		LatestWords:      []string{"ultimo", "ultima", "ultimos", "recente", "recentes"},
		StatePhrases:     []string{"proximos passos", "proximo passo", "estado atual", "situacao atual", "estado do processo"},
		StopWords:        []string{"o", "a", "os", "as", "um", "uma", "uns", "umas", "e", "ou", "de", "do", "da", "dos", "das", "ao", "aos", "em", "no", "na", "nos", "nas", "por", "para", "com", "sem", "sobre", "e", "sao", "era", "foi", "ser", "estar", "este", "esta", "isto", "esse", "essa", "isso", "se", "seu", "sua", "seus", "suas", "meu", "minha", "que", "muito", "mais", "mas", "sim", "nao", "ja", "ha", "me", "te", "lhe"},
		PageWords:        []string{"pagina", "paginas", "pag", "p"},
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

// Detect returns the language of a text: the one with the most stop words in
// it. It returns false when the text has no stop words.
func Detect(text string) (Language, bool) {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	best, bestN := -1, 0
	for i, l := range All {
		n := 0
		for _, w := range words {
			if slices.Contains(l.StopWords, fold(w)) {
				n++
			}
		}
		if n > bestN {
			best, bestN = i, n
		}
	}
	if best < 0 {
		return Language{}, false
	}
	return All[best], true
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

// PageRE matches a page reference in any supported language, such as
// "página 6", "page 6", "pág. 6" or "p. 6". Use it on text without accents.
var PageRE = func() *regexp.Regexp {
	seen := map[string]bool{}
	var words []string
	for _, l := range All {
		for _, w := range l.PageWords {
			if !seen[w] {
				seen[w] = true
				words = append(words, regexp.QuoteMeta(w))
			}
		}
	}
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(words, "|") + `)\.?\s*(\d{1,4})\b`)
}()

// Pages returns the page numbers that a text refers to, such as 6 in
// "las fotos de la página 6".
func Pages(text string) []int {
	var out []int
	for _, m := range PageRE.FindAllStringSubmatch(fold(text), -1) {
		var n int
		if _, err := fmt.Sscan(m[1], &n); err == nil && n > 0 && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// AsksLatest reports whether a text asks for the newest documents, in any
// supported language: "el último correo", "the latest email", or how the
// case stands now: "¿cuáles son los siguientes pasos?".
func AsksLatest(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(fold(text)), func(r rune) bool { return !unicode.IsLetter(r) })
	joined := " " + strings.Join(words, " ") + " "
	for _, l := range All {
		for _, w := range words {
			if slices.Contains(l.LatestWords, w) {
				return true
			}
		}
		for _, p := range l.StatePhrases {
			if strings.Contains(joined, " "+p+" ") {
				return true
			}
		}
	}
	return false
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

// ReplyRE matches the line above a quoted reply in any supported language,
// such as "On Wed, Oct 21, 2026 at 9:10 AM Ana <ana@example.com> wrote:" or
// "El mie, 29 oct 2026 a las 18:00, Ana (<ana@example.com>) escribio:".
// Group 1 holds the date and the sender.
var ReplyRE = func() *regexp.Regexp {
	var intros, verbs []string
	for _, l := range All {
		intros = append(intros, regexp.QuoteMeta(l.Reply[0]))
		verbs = append(verbs, regexp.QuoteMeta(l.Reply[1]))
	}
	return regexp.MustCompile(`^(?:` + strings.Join(intros, "|") + `)\s+(.{6,200}?)\s+(?:` + strings.Join(verbs, "|") + `)\s*:\s*$`)
}()

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

package lang

import (
	"slices"
	"testing"
)

func TestAll(t *testing.T) {
	if got := Codes(); !slices.Equal(got, []string{"en", "es", "fr", "pt"}) {
		t.Errorf("codes = %v", got)
	}
	headerFields := []string{"from", "date", "to", "cc", "subject"}
	for _, l := range All {
		if l.Tesseract == "" || len(l.ForwardMarkers) == 0 || len(l.ChatAttached) == 0 || len(l.ChatFileAttached) == 0 {
			t.Errorf("%s: missing data", l.Code)
		}
		for _, field := range []string{"from", "date", "subject"} {
			if !slices.Contains(mapValues(l.Headers), field) {
				t.Errorf("%s: no header for %s", l.Code, field)
			}
		}
		for k, v := range l.Headers {
			if !slices.Contains(headerFields, v) {
				t.Errorf("%s: header %q maps to unknown field %q", l.Code, k, v)
			}
		}
	}
	// A header name shared by two languages must mean the same field.
	seen := map[string]string{}
	for _, l := range All {
		for k, v := range l.Headers {
			if old, ok := seen[k]; ok && old != v {
				t.Errorf("header %q is %q in one language and %q in another", k, old, v)
			}
			seen[k] = v
		}
	}
}

func TestGet(t *testing.T) {
	got, err := Get([]string{"ES", " pt"})
	if err != nil || len(got) != 2 || got[0].Tesseract != "spa" || got[1].Tesseract != "por" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	if _, err := Get([]string{"de"}); err == nil {
		t.Error("de is not supported")
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestStopWords(t *testing.T) {
	sw := AllStopWords()
	for _, w := range []string{"the", "de", "que", "les", "nao"} {
		if !sw[w] {
			t.Errorf("%q is not a stop word", w)
		}
	}
	for _, l := range All {
		for _, w := range l.StopWords {
			for _, r := range w {
				if r > 127 {
					t.Errorf("%s stop word %q has an accent; write it folded", l.Code, w)
				}
			}
		}
	}
	// Question words that carry meaning stay searchable.
	for _, w := range []string{"quien", "fotos", "donde", "where", "quand", "onde"} {
		if sw[w] {
			t.Errorf("%q must not be a stop word", w)
		}
	}
}

func TestMonths(t *testing.T) {
	// A name shared by two languages must be the same month in both.
	seen := map[string]int{}
	for _, l := range All {
		for i, names := range l.Months {
			if len(names) == 0 {
				t.Errorf("%s: no names for month %d", l.Code, i+1)
			}
			for _, n := range names {
				if old, ok := seen[n]; ok && old != i+1 {
					t.Errorf("%q is month %d and %d", n, old, i+1)
				}
				seen[n] = i + 1
			}
		}
	}
	m := MonthNumbers()
	for name, want := range map[string]int{"oct": 10, "outubro": 10, "aout": 8, "dic": 12, "dez": 12, "may": 5, "mai": 5} {
		if m[name] != want {
			t.Errorf("%s = %d, want %d", name, m[name], want)
		}
	}
}

func TestDetect(t *testing.T) {
	for text, want := range map[string]string{
		"¿Quién aportó las fotos de la solicitud?":          "es",
		"Who sent the photos of the request?":               "en",
		"Qui a envoyé les photos de la demande ?":           "fr",
		"Quem enviou as fotos do pedido? Não sei.":          "pt",
		"Where is the meter room and who has the key to it": "en",
	} {
		l, ok := Detect(text)
		if !ok || l.Code != want {
			t.Errorf("Detect(%q) = %q, %v, want %q", text, l.Code, ok, want)
		}
	}
	if _, ok := Detect("12345 ???"); ok {
		t.Error("no language in numbers")
	}
}

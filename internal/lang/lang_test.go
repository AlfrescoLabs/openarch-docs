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

package email

import (
	"os"
	"strings"
	"testing"
)

func parseFile(t *testing.T, name string) *Message {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	m, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseNested(t *testing.T) {
	m := parseFile(t, "nested.eml")
	if m.Info.Subject != "Fwd: Condiciones de suministro nº 9999" {
		t.Errorf("subject = %q", m.Info.Subject)
	}
	if m.Info.From != "Ana Lopez <ana@example.com>" || m.Info.Date != "2026-10-30T07:51:00+01:00" || m.Info.MessageID != "outer-1@example.com" {
		t.Errorf("info = %+v", m.Info)
	}
	// The plain text form wins over the HTML form; the HTML is kept apart.
	if !strings.HasPrefix(m.Text, "Te reenvío las condiciones.") || strings.Count(m.Text, "Te reenvío") != 1 {
		t.Errorf("text = %q", m.Text)
	}
	if !strings.Contains(m.HTML, "<img") {
		t.Errorf("html = %q", m.HTML)
	}
	if len(m.Parts) != 1 || !m.Parts[0].Inline || m.Parts[0].MediaType != "image/png" || m.Parts[0].Number != "1.2" {
		t.Fatalf("parts = %+v", m.Parts)
	}
	if !strings.HasPrefix(string(m.Parts[0].Data), "\x89PNG") {
		t.Error("inline image not decoded")
	}
	if len(m.Nested) != 1 {
		t.Fatalf("nested = %d", len(m.Nested))
	}
	n := m.Nested[0]
	if n.Number != "2" || !strings.HasPrefix(string(n.Raw), "From: =?ISO-8859-1?Q?") {
		t.Errorf("nested number %q raw %q", n.Number, string(n.Raw)[:20])
	}
	if n.Message.Info.Subject != "Contestación expediente 9999" || n.Message.Info.From != "Distribuidora Ejemplo <avisos@example.com>" {
		t.Errorf("nested info = %+v", n.Message.Info)
	}
	if n.Message.Text != "La centralización concedida está en Calle Falsa 12, sótano -2." {
		t.Errorf("nested text = %q", n.Message.Text)
	}
	if len(n.Message.Parts) != 1 || n.Message.Parts[0].Filename != "Condiciones técnicas.pdf" || n.Message.Parts[0].Inline {
		t.Fatalf("nested parts = %+v", n.Message.Parts)
	}
	if string(n.Message.Parts[0].Data) != "%PDF-1.4 fake" {
		t.Error("attachment not decoded")
	}
}

func TestParseHTMLOnly(t *testing.T) {
	m := parseFile(t, "html-only.eml")
	want := "Primera línea\n\nA\tB\n\nDe: Otro Remitente <otro@example.com>\nAsunto: Re: Aviso\nFecha: 1 nov 2026"
	if m.Text != want {
		t.Errorf("text = %q, want %q", m.Text, want)
	}
	if strings.Contains(m.Text, "x()") {
		t.Error("script text kept")
	}
}

func TestForwarded(t *testing.T) {
	m := parseFile(t, "nested.eml")
	got := Forwarded(m.Text)
	if len(got) != 1 || got[0].From != "Instalador Ejemplo <instalador@example.net>" || got[0].Subject != "Condiciones de suministro" ||
		got[0].Date != "mié, 29 oct 2026 a las 18:00" || got[0].To != "Ana Lopez <ana@example.com>" {
		t.Errorf("forwarded = %+v", got)
	}
	// Outlook style in Spanish, with no marker line, and two quoted messages.
	text := "Hola\n\nDe: Uno <uno@example.com>\nEnviado: lunes, 3 de noviembre de 2026 9:00\nPara: Dos\nAsunto: RE: Plano\n\ntexto\n> De: Dos <dos@example.com>\n> Fecha: 2 nov 2026\n> Asunto: Plano\n"
	got = Forwarded(text)
	if len(got) != 2 || got[0].From != "Uno <uno@example.com>" || got[0].Date != "lunes, 3 de noviembre de 2026 9:00" || got[1].Subject != "Plano" {
		t.Errorf("outlook forwarded = %+v", got)
	}
	// A From line alone is not a quoted message.
	if got := Forwarded("From: nadie\nTexto normal"); len(got) != 0 {
		t.Errorf("false positive: %+v", got)
	}
}

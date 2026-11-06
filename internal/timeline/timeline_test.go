package timeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
)

func TestParseDate(t *testing.T) {
	tests := map[string]string{
		"2026-10-29T12:00:00+01:00":                "2026-10-29T12:00",
		"2026-11-04T16:54":                         "2026-11-04T16:54",
		"2026-11-04T16:54:30":                      "2026-11-04T16:54",
		"Fri, Oct 30, 2026 at 5:51 PM":             "2026-10-30T17:51",
		"October 29, 2026 at 12:05 AM":             "2026-10-29T00:05",
		"mié, 29 oct 2026 a las 18:00":             "2026-10-29T18:00",
		"mar, 4 nov 2026 a las 17:30":              "2026-11-04T17:30",
		"mar. 4 nov. 2026 à 17:30":                 "2026-11-04T17:30",
		"mar, 3 mar 2026 a las 9:00":               "2026-03-03T09:00",
		"jueves, 30 de octubre de 2026 7:51":       "2026-10-30T07:51",
		"lunes, 3 de noviembre de 2026 9:00":       "2026-11-03T09:00",
		"mer. 29 oct. 2026 à 18:00":                "2026-10-29T18:00",
		"mercredi 29 octobre 2026 18h05":           "2026-10-29T18:05",
		"29 août 2026 10:00":                       "2026-08-29T10:00",
		"qua., 29 de out. de 2026 às 18:00":        "2026-10-29T18:00",
		"29 de outubro de 2026 18:00":              "2026-10-29T18:00",
		"30/10/2026 7:51":                          "2026-10-30T07:51",
		"10/30/2026":                               "2026-10-30T00:00",
		"Thu, 30 Oct 2026 07:51:00 +0100":          "2026-10-30T07:51",
		"martes, 4 de noviembre de 2026, 17:41:00": "2026-11-04T17:41",
		"":                      "",
		"pronto":                "",
		"31 de febrero de 2026": "",
	}
	for in, want := range tests {
		if got := ParseDate(in); got != want {
			t.Errorf("ParseDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuild(t *testing.T) {
	root := t.TempDir()
	eml, err := os.ReadFile("../email/testdata/nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	// The quoted forward in the outer email is also the date of no email here,
	// and the nested email has its own date.
	if err := os.WriteFile(filepath.Join(root, "a.eml"), eml, 0o644); err != nil {
		t.Fatal(err)
	}
	// The same nested email again, as its own file: listed once.
	inner := "From: =?ISO-8859-1?Q?Distribuidora_Ejemplo?= <avisos@example.com>\nSubject: Copia\nDate: Wed, 29 Oct 2026 12:00:00 +0100\n\ncopia\n"
	if err := os.WriteFile(filepath.Join(root, "b.eml"), []byte(inner), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	events, err := Build(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range events {
		got = append(got, e.Time+" "+e.Kind+" "+address(e.From))
	}
	want := []string{
		"2026-10-29T12:00 email avisos@example.com",
		"2026-10-29T18:00 forwarded instalador@example.net",
		"2026-10-30T07:51 email ana@example.com",
	}
	if len(got) != len(want) {
		t.Fatalf("events = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
	events, _ = Build(Options{Root: root, Since: "2026-10-30", Kinds: []string{KindEmail}})
	if len(events) != 1 || events[0].From != "Ana Lopez <ana@example.com>" {
		t.Errorf("filtered = %+v", events)
	}
}

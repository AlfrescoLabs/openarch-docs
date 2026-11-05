package chat

import "testing"

func TestParseIOS(t *testing.T) {
	text := "\ufeff[24/8/26, 18:05:36] Instalador: Hola\n" +
		"[24/8/26, 18:06:00] Cliente: Mañana miro\nel cuarto de contadores\n" +
		"\u200e[10/9/26, 18:10:04] Instalador: \u200e<adjunto: 00000023-PHOTO-2026-09-10-18-10-04.jpg>\n"
	got := Parse(text)
	if len(got) != 3 {
		t.Fatalf("got %d messages: %+v", len(got), got)
	}
	if got[0].Sender != "Instalador" || got[0].Text != "Hola" || got[0].Time != "24/8/26, 18:05:36" || got[0].ISO != "2026-08-24T18:05:36" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Text != "Mañana miro\nel cuarto de contadores" {
		t.Errorf("continuation = %q", got[1].Text)
	}
	if got[2].Attachment != "00000023-PHOTO-2026-09-10-18-10-04.jpg" || got[2].ISO != "2026-09-10T18:10:04" {
		t.Errorf("attachment = %+v", got[2])
	}
}

func TestParseAndroid(t *testing.T) {
	text := "9/13/26, 6:05 PM - Ana: IMG-20260913-WA0001.jpg (file attached)\n" +
		"9/14/26, 9:00 AM - Luis: ok\n"
	got := Parse(text)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Attachment != "IMG-20260913-WA0001.jpg" || got[0].ISO != "2026-09-13T18:05:00" || got[0].Sender != "Ana" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ISO != "2026-09-14T09:00:00" {
		t.Errorf("second = %+v", got[1])
	}
}

func TestParseUnknownOrder(t *testing.T) {
	// Every day and month is 12 or less, so the date order is not known.
	got := Parse("[1/2/26, 10:00:00] A: x\n")
	if len(got) != 1 || got[0].ISO != "" || got[0].Time != "1/2/26, 10:00:00" {
		t.Errorf("got %+v", got)
	}
}

func TestIsChatFile(t *testing.T) {
	for name, want := range map[string]bool{
		"_chat.txt": true, "x/_chat.txt": true, "WhatsApp Chat with Ana.txt": true, "notes.txt": false, "_chat.txt.jpg": false,
	} {
		if IsChatFile(name) != want {
			t.Errorf("IsChatFile(%q) = %v", name, !want)
		}
	}
}

func TestParseLanguages(t *testing.T) {
	tests := []struct{ line, sender, attachment, iso string }{
		{"[24/08/2026 18:05:36] Marie: <pièce jointe : 00000001-PHOTO-2026-08-24-18-05-36.jpg>", "Marie", "00000001-PHOTO-2026-08-24-18-05-36.jpg", "2026-08-24T18:05:36"},
		{"24/08/2026 à 18:05 - Marie: IMG-20260824-WA0001.jpg (fichier joint)", "Marie", "IMG-20260824-WA0001.jpg", "2026-08-24T18:05:00"},
		{"[24/08/26, 18:05:36] João: <anexado: 00000002-PHOTO-2026-08-24-18-05-36.jpg>", "João", "00000002-PHOTO-2026-08-24-18-05-36.jpg", "2026-08-24T18:05:36"},
		{"24/08/2026 18:05 - João: IMG-20260824-WA0002.jpg (arquivo anexado)", "João", "IMG-20260824-WA0002.jpg", "2026-08-24T18:05:00"},
		{"24/8/26, 18:05 - Ana: IMG-20260824-WA0003.jpg (archivo adjunto)", "Ana", "IMG-20260824-WA0003.jpg", "2026-08-24T18:05:00"},
	}
	for _, tt := range tests {
		got := Parse(tt.line + "\n")
		if len(got) != 1 || got[0].Sender != tt.sender || got[0].Attachment != tt.attachment || got[0].ISO != tt.iso {
			t.Errorf("%s: got %+v", tt.line, got)
		}
	}
	for _, name := range []string{"Chat de WhatsApp con Ana.txt", "Discussion WhatsApp avec Marie.txt", "Conversa do WhatsApp com João.txt"} {
		if !IsChatFile(name) {
			t.Errorf("IsChatFile(%q) = false", name)
		}
	}
}

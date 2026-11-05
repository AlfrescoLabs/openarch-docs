package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScript(t *testing.T) {
	got := Script([]Cmd{
		{Args: []string{"pdfinfo", In}, Stdout: "info.txt"},
		{Args: []string{"echo", "it's"}},
	})
	want := "set -e\n'pdfinfo' '/in/input' > '/out/info.txt'\n'echo' 'it'\\''s'"
	if got != want {
		t.Errorf("Script = %q, want %q", got, want)
	}
}

func TestBindMount(t *testing.T) {
	got := bindMount(`/a,b:c/"q".pdf`, In, true)
	want := `type=bind,"source=/a,b:c/""q"".pdf",target=/in/input,readonly`
	if got != want {
		t.Errorf("bindMount = %q, want %q", got, want)
	}
}

func TestLocalPath(t *testing.T) {
	out := filepath.Join("tmp", "work")
	if got := localPath(In, "doc.pdf", out); got != "doc.pdf" {
		t.Errorf("In = %q", got)
	}
	if got := localPath(Out+"/page-1", "doc.pdf", out); got != filepath.Join(out, "page-1") {
		t.Errorf("Out = %q", got)
	}
	if got := localPath("-r", "doc.pdf", out); got != "-r" {
		t.Errorf("flag = %q", got)
	}
}

func TestImage(t *testing.T) {
	if !strings.HasPrefix(Image, "openarch-docs-tools:") || len(Image) != len("openarch-docs-tools:")+12 {
		t.Errorf("Image = %q", Image)
	}
}

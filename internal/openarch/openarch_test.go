package openarch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCLI writes a shell script that answers like openarch.
func fakeCLI(t *testing.T, script string) *CLI {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake openarch is a shell script")
	}
	bin := filepath.Join(t.TempDir(), "openarch")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &CLI{Bin: bin, Context: "test"}
}

func TestCLI(t *testing.T) {
	c := fakeCLI(t, `echo "$@" > "$(dirname "$0")/args"
case "$2" in
get) echo '{"sys_id":"d1","sys_path":"/a","sys_isFolderish":true}' ;;
list) echo '{"documents":[{"sys_id":"d2"}],"totalCount":1}' ;;
*) echo '{"error":{"code":"remote","message":"get document /x: not found: /x"}}'; exit 5 ;;
esac
`)
	d, err := c.Get(context.Background(), "/a")
	if err != nil || d.ID != "d1" || !d.Folderish {
		t.Fatalf("Get = %+v, %v", d, err)
	}
	args, _ := os.ReadFile(filepath.Join(filepath.Dir(c.Bin), "args"))
	if strings.TrimSpace(string(args)) != "documents get /a --output json --non-interactive --context test" {
		t.Errorf("args = %q", args)
	}
	docs, err := c.List(context.Background(), "/a")
	if err != nil || len(docs) != 1 {
		t.Errorf("List = %+v, %v", docs, err)
	}
	_, err = c.Upload(context.Background(), "f", "/a", "f", "d")
	if !IsNotFound(err) {
		t.Errorf("err = %v", err)
	}
	var e *Error
	if !errors.As(err, &e) || e.Exit != 5 || e.Code != "remote" {
		t.Errorf("error = %+v", e)
	}
}

func TestCLINoJSON(t *testing.T) {
	c := fakeCLI(t, "echo boom >&2; exit 6\n")
	_, err := c.Get(context.Background(), "/a")
	var e *Error
	if !errors.As(err, &e) || e.Exit != 6 || !strings.Contains(e.Message, "boom") || IsNotFound(err) {
		t.Errorf("err = %#v", err)
	}
}

package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"openarch-docs": func() { os.Exit(run()) },
	})
}

func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("..", "..", "testdata", "script"),
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"mkzip": mkzip,
		},
	})
}

// mkzip out.zip file... writes a zip with the files, stored under their base names.
func mkzip(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: mkzip out.zip file...")
	}
	f, err := os.Create(ts.MkAbs(args[0]))
	ts.Check(err)
	zw := zip.NewWriter(f)
	for _, name := range args[1:] {
		w, err := zw.Create(filepath.Base(name))
		ts.Check(err)
		_, err = w.Write([]byte(ts.ReadFile(name)))
		ts.Check(err)
	}
	ts.Check(zw.Close())
	ts.Check(f.Close())
}

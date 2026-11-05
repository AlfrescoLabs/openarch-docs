// Package verify checks that the originals and the derived files of a case
// folder still match the checksums in the manifest.
package verify

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
)

// Problem kinds.
const (
	ProblemMissing = "missing"
	ProblemChanged = "changed"
	ProblemZip     = "zip"
)

// Problem is one file that does not match the manifest.
type Problem struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// Result is the outcome of a check.
type Result struct {
	Root      string    `json:"root"`
	Originals int       `json:"originals"`
	Derived   int       `json:"derived"`
	Problems  []Problem `json:"problems"`
	// New lists originals that are not in the manifest yet. They are not problems.
	New []string `json:"new"`
}

// OK reports whether every file matches.
func (r *Result) OK() bool { return len(r.Problems) == 0 }

// Run checks the case folder root.
func Run(root string) (*Result, error) {
	m, err := casefile.Load(root)
	if err != nil {
		return nil, err
	}
	res := &Result{Root: root, Originals: len(m.Originals), Derived: len(m.Derived), Problems: []Problem{}, New: []string{}}
	for _, o := range m.Originals {
		if p, bad := check(root, o.Path, o.SHA256); bad {
			res.Problems = append(res.Problems, p)
			continue
		}
		if o.Kind == casefile.KindZip {
			if err := checkZip(filepath.Join(root, filepath.FromSlash(o.Path))); err != nil {
				res.Problems = append(res.Problems, Problem{Path: o.Path, Kind: ProblemZip, Detail: err.Error()})
			}
		}
	}
	for _, d := range m.Derived {
		if p, bad := check(root, d.Path, d.SHA256); bad {
			res.Problems = append(res.Problems, p)
		}
	}
	paths, err := casefile.FindOriginals(root)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if !slices.ContainsFunc(m.Originals, func(o casefile.Original) bool { return o.Path == p }) {
			res.New = append(res.New, p)
		}
	}
	return res, nil
}

func check(root, p, want string) (Problem, bool) {
	got, _, err := casefile.HashFile(filepath.Join(root, filepath.FromSlash(p)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Problem{Path: p, Kind: ProblemMissing}, true
	case err != nil:
		return Problem{Path: p, Kind: ProblemMissing, Detail: err.Error()}, true
	case got != want:
		return Problem{Path: p, Kind: ProblemChanged, Detail: "sha256 " + got + ", manifest " + want}, true
	}
	return Problem{}, false
}

// checkZip reads every file of a zip, so the CRC of each one is checked.
func checkZip(p string) error {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, rc)
		_ = rc.Close()
		if err != nil {
			return errors.New(f.Name + ": " + err.Error())
		}
	}
	return nil
}

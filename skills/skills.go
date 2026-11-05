// Package skills embeds the Agent Skill of openarch-docs.
package skills

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
)

// SKILL is the skill file.
//
//go:embed openarch-docs/SKILL.md
var SKILL []byte

// Results of Install.
const (
	Installed = "installed"
	Unchanged = "unchanged"
)

// ErrChanged means the installed skill differs and force was not set.
var ErrChanged = errors.New("the installed SKILL.md differs from this release")

// Path returns where the skill goes under dir.
func Path(dir string) string {
	return filepath.Join(dir, ".claude", "skills", "openarch-docs", "SKILL.md")
}

// Install writes the skill under dir. A different file is replaced only with force.
func Install(dir string, force bool) (string, string, error) {
	p := Path(dir)
	old, err := os.ReadFile(p)
	switch {
	case err == nil && string(old) == string(SKILL):
		return p, Unchanged, nil
	case err == nil && !force:
		return p, "", ErrChanged
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return p, "", err
	}
	return p, Installed, os.WriteFile(p, SKILL, 0o644)
}

package skills

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSkill(t *testing.T) {
	s := string(SKILL)
	if !strings.HasPrefix(s, "---\nname: openarch-docs\ndescription: ") {
		t.Fatal("bad front matter")
	}
	desc := strings.SplitN(strings.SplitN(s, "description: ", 2)[1], "\n", 2)[0]
	if len(desc) > 1024 {
		t.Errorf("description is %d characters, max 1024", len(desc))
	}
	// The skill is ASCII, except the letters it names as examples.
	for i, r := range strings.NewReplacer("ñ", "", "ç", "", "ã", "").Replace(s) {
		if r > 127 {
			t.Errorf("non-ASCII %q at %d", r, i)
			break
		}
	}
}

func TestInstall(t *testing.T) {
	dir := t.TempDir()
	if _, res, err := Install(dir, false); err != nil || res != Installed {
		t.Fatalf("install = %s, %v", res, err)
	}
	if _, res, err := Install(dir, false); err != nil || res != Unchanged {
		t.Fatalf("again = %s, %v", res, err)
	}
	if err := os.WriteFile(Path(dir), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Install(dir, false); !errors.Is(err, ErrChanged) {
		t.Errorf("changed = %v", err)
	}
	if _, res, err := Install(dir, true); err != nil || res != Installed {
		t.Errorf("force = %s, %v", res, err)
	}
}

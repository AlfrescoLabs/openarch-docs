package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AlfrescoLabs/openarch-docs/internal/index"
	"github.com/AlfrescoLabs/openarch-docs/internal/ingest"
)

func newCase(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	eml, err := os.ReadFile("../email/testdata/nested.eml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reenvio.eml"), eml, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Run(ingest.Options{Root: root}); err != nil {
		t.Fatal(err)
	}
	x, err := index.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = x.Close() }()
	if _, err := x.Build(context.Background(), index.BuildOptions{Root: root}); err != nil {
		t.Fatal(err)
	}
	return root
}

func session(t *testing.T, opts Options) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := New(opts).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call runs a tool and returns its text and whether it is an error.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res.Content[0].(*mcp.TextContent).Text, res.IsError
}

func TestTools(t *testing.T) {
	root := newCase(t)
	cs := session(t, Options{Root: root, Version: "test"})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	// No vision model: no look.
	if strings.Join(names, ",") != "collect,files,note_write,read,search,status,timeline,verify" {
		t.Errorf("tools = %v", names)
	}

	text, isErr := call(t, cs, "search", map[string]any{"query": "sotano centralizacion"})
	var sr struct {
		Hits []index.Hit `json:"hits"`
	}
	if isErr || json.Unmarshal([]byte(text), &sr) != nil || len(sr.Hits) == 0 || !strings.Contains(sr.Hits[0].Text, "sótano") {
		t.Fatalf("search = %s", text)
	}

	text, isErr = call(t, cs, "read", map[string]any{"path": sr.Hits[0].Path, "max_chars": 40})
	var rr struct {
		Text       string          `json:"text"`
		NextOffset int             `json:"next_offset"`
		Provenance json.RawMessage `json:"provenance"`
	}
	if isErr || json.Unmarshal([]byte(text), &rr) != nil || len([]rune(rr.Text)) != 40 || rr.NextOffset != 40 || !strings.Contains(string(rr.Provenance), "Distribuidora Ejemplo") {
		t.Errorf("read = %s", text)
	}
	for _, bad := range []string{"../x.txt", "/etc/passwd", "reenvio.eml/../../x.txt", "extracted/manifest.json"} {
		if text, isErr := call(t, cs, "read", map[string]any{"path": bad}); !isErr {
			t.Errorf("read %s = %s", bad, text)
		}
	}

	text, isErr = call(t, cs, "files", map[string]any{"kind": "attachment"})
	if isErr || !strings.Contains(text, `"attachment": "Condiciones técnicas.pdf"`) || !strings.Contains(text, `"email": "Contestación expediente 9999"`) {
		t.Errorf("files = %s", text)
	}

	text, isErr = call(t, cs, "timeline", map[string]any{"kinds": []string{"email"}})
	if isErr || strings.Count(text, `"kind": "email"`) != 2 {
		t.Errorf("timeline = %s", text)
	}

	text, isErr = call(t, cs, "note_write", map[string]any{"name": "drafts/respuesta", "text": "# Borrador\n\nGracias."})
	if isErr || !strings.Contains(text, "notes/drafts/respuesta.md") {
		t.Fatalf("note_write = %s", text)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "notes/drafts/respuesta.md")); string(b) != "# Borrador\n\nGracias.\n" {
		t.Errorf("note = %q", b)
	}
	if _, isErr := call(t, cs, "note_write", map[string]any{"name": "drafts/respuesta", "text": "otra"}); !isErr {
		t.Error("overwrite without overwrite: true")
	}
	for _, bad := range []string{"../escape", "a/b/c", "/abs", "..md"} {
		if _, isErr := call(t, cs, "note_write", map[string]any{"name": bad, "text": "x"}); !isErr {
			t.Errorf("note name %q accepted", bad)
		}
	}

	text, isErr = call(t, cs, "files", map[string]any{"kind": "note"})
	if isErr || !strings.Contains(text, `"path": "notes/drafts/respuesta.md"`) || !strings.Contains(text, `"total": 1`) {
		t.Errorf("notes = %s", text)
	}

	if text, isErr := call(t, cs, "verify", map[string]any{}); isErr || !strings.Contains(text, `"problems": []`) {
		t.Errorf("verify = %s", text)
	}
}

func TestReadOnly(t *testing.T) {
	cs := session(t, Options{Root: newCase(t), ReadOnly: true})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "note_write" || tool.Name == "collect" {
			t.Error("note_write in read-only mode")
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
	}
}

func TestResultCut(t *testing.T) {
	res, _, _ := result(map[string]string{"text": strings.Repeat("ñ", maxResult)})
	if !res.IsError {
		t.Error("a result over maxResult is not an error")
	}

	root := t.TempDir()
	dir := filepath.Join(root, "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 200 {
		name := fmt.Sprintf("%03d-%s.md", i, strings.Repeat("n", 100))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Quotes double in JSON, so 12000 characters give more than maxResult.
	if err := os.WriteFile(filepath.Join(dir, "quotes.md"), []byte(strings.Repeat(`"`, 12000)), 0o644); err != nil {
		t.Fatal(err)
	}
	cs := session(t, Options{Root: root, Version: "test"})

	text, isErr := call(t, cs, "files", map[string]any{"limit": 200})
	var files struct {
		Files      []map[string]any `json:"files"`
		Cut        bool             `json:"cut"`
		NextOffset int              `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(text), &files); err != nil || isErr || len(text) > maxResult {
		t.Fatalf("files: %v, %d bytes", err, len(text))
	}
	if !files.Cut || len(files.Files) == 0 || len(files.Files) == 200 || files.NextOffset != len(files.Files) {
		t.Errorf("files: cut %v, %d files, next %d", files.Cut, len(files.Files), files.NextOffset)
	}

	text, isErr = call(t, cs, "read", map[string]any{"path": "notes/quotes.md", "max_chars": 12000})
	var read struct {
		Text       string `json:"text"`
		NextOffset int    `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(text), &read); err != nil || isErr || len(text) > maxResult {
		t.Fatalf("read: %v, %d bytes", err, len(text))
	}
	if read.Text == "" || read.NextOffset != len(read.Text) {
		t.Errorf("read: %d chars, next %d", len(read.Text), read.NextOffset)
	}
}

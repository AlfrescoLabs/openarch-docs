// Package openarch talks to OpenArch by running the openarch CLI with
// --output json. The plugin never sees a password: openarch reads its own
// credential store, workspace and context.
package openarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Doc is a document as openarch prints it.
type Doc struct {
	ID          string `json:"sys_id"`
	Type        string `json:"sys_primaryType"`
	Name        string `json:"sys_name"`
	Title       string `json:"sys_title"`
	Path        string `json:"sys_path"`
	Description string `json:"sys_description"`
	Folderish   bool   `json:"sys_isFolderish"`
	Blob        *struct {
		Digest string `json:"digest"`
		Length int64  `json:"length"`
	} `json:"sysfile_blob"`
}

// Client is what the plugin needs from OpenArch. Tests use a fake.
type Client interface {
	Get(ctx context.Context, ref string) (Doc, error)
	CreateFolder(ctx context.Context, parent, name string) (Doc, error)
	// Upload puts a file under parent; a file document with the same name
	// gets the new content.
	Upload(ctx context.Context, file, parent, name, description string) (Doc, error)
	// List returns the children of a folder.
	List(ctx context.Context, folder string) ([]Doc, error)
	Download(ctx context.Context, ref, out string) error
}

// Error is an error that openarch printed.
type Error struct {
	Exit    int
	Code    string
	Message string
	Hint    string
}

func (e *Error) Error() string {
	if e.Hint != "" {
		return e.Message + " (" + e.Hint + ")"
	}
	return e.Message
}

// IsNotFound reports whether err is an openarch "not found" error.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "remote" && strings.Contains(e.Message, "not found")
}

// ErrNoCLI means the openarch command is not found.
var ErrNoCLI = errors.New("the openarch command is not on PATH and OPENARCH_BIN is not set")

// CLI runs the openarch command.
type CLI struct {
	Bin string
	// Context is the openarch connection context, or "" for its default.
	Context string
}

// NewCLI finds openarch: OPENARCH_BIN, which openarch sets when it runs the
// plugin, else openarch on PATH.
func NewCLI(contextName string) (*CLI, error) {
	bin := os.Getenv("OPENARCH_BIN")
	if bin == "" {
		p, err := exec.LookPath("openarch")
		if err != nil {
			return nil, ErrNoCLI
		}
		bin = p
	}
	return &CLI{Bin: bin, Context: contextName}, nil
}

// run runs openarch with args and decodes its JSON output into out.
func (c *CLI) run(ctx context.Context, out any, args ...string) error {
	args = append(args, "--output", "json", "--non-interactive")
	if c.Context != "" {
		args = append(args, "--context", c.Context)
	}
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Hint    string `json:"hint"`
			} `json:"error"`
		}
		if json.Unmarshal(stdout.Bytes(), &e) == nil && e.Error.Message != "" {
			return &Error{Exit: exitErr.ExitCode(), Code: e.Error.Code, Message: e.Error.Message, Hint: e.Error.Hint}
		}
		return &Error{Exit: exitErr.ExitCode(), Code: "error", Message: "openarch " + args[0] + ": " + strings.TrimSpace(stderr.String())}
	}
	if err != nil {
		return fmt.Errorf("run openarch: %w", err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("openarch %s: output is not JSON: %w", args[0], err)
	}
	return nil
}

// Get implements Client.
func (c *CLI) Get(ctx context.Context, ref string) (Doc, error) {
	var d Doc
	err := c.run(ctx, &d, "documents", "get", ref)
	return d, err
}

// CreateFolder implements Client.
func (c *CLI) CreateFolder(ctx context.Context, parent, name string) (Doc, error) {
	var d Doc
	err := c.run(ctx, &d, "documents", "create", "--parent", parent, "--type", "SysFolder", "--name", name, "--title", name)
	return d, err
}

// Upload implements Client.
func (c *CLI) Upload(ctx context.Context, file, parent, name, description string) (Doc, error) {
	var d Doc
	err := c.run(ctx, &d, "documents", "upload", file, "--parent", parent, "--name", name, "--title", name,
		"--description", description, "--replace")
	return d, err
}

// listPage is the number of children asked for at once.
const listPage = 500

// List implements Client.
func (c *CLI) List(ctx context.Context, folder string) ([]Doc, error) {
	var all []Doc
	for offset := 0; ; offset += listPage {
		var page struct {
			Documents  []Doc `json:"documents"`
			TotalCount int   `json:"totalCount"`
		}
		if err := c.run(ctx, &page, "documents", "list", folder, "--limit", strconv.Itoa(listPage), "--offset", strconv.Itoa(offset)); err != nil {
			return nil, err
		}
		all = append(all, page.Documents...)
		if len(page.Documents) < listPage || len(all) >= page.TotalCount {
			return all, nil
		}
	}
}

// Download implements Client.
func (c *CLI) Download(ctx context.Context, ref, out string) error {
	return c.run(ctx, nil, "documents", "content", ref, "--out", out)
}

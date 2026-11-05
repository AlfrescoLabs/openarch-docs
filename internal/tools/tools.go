// Package tools runs the PDF and OCR programs (poppler and tesseract), in the
// tools image with no network, or from the host PATH.
package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed Dockerfile
var dockerfile []byte

// Image is the tag of the tools image. It changes when the Dockerfile changes.
var Image = func() string {
	sum := sha256.Sum256(dockerfile)
	return "openarch-docs-tools:" + hex.EncodeToString(sum[:])[:12]
}()

// Programs lists the programs the tools need.
var Programs = []string{"pdfinfo", "pdftotext", "pdfimages", "pdftoppm", "tesseract"}

// Paths inside a run. A command names its input as In and its output files under Out.
const (
	In  = "/in/input"
	Out = "/out"
)

// Cmd is one program call. When Stdout is set, the output goes to that file under Out.
type Cmd struct {
	Args   []string
	Stdout string
}

// Runner runs commands on one input file. Commands write their files to outDir.
type Runner interface {
	Run(ctx context.Context, input, outDir string, cmds []Cmd) error
}

// Docker runs commands in the tools image, with the input file mounted read
// only and no network.
type Docker struct {
	// Log receives the output of 'docker build'.
	Log io.Writer
}

// Local runs commands from the host PATH.
type Local struct{}

// EnsureImage builds the tools image when it is missing. Only the build
// downloads packages; runs have no network.
func (d Docker) EnsureImage(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return ErrNoDocker
	}
	if exec.CommandContext(ctx, "docker", "image", "inspect", Image).Run() == nil {
		return nil
	}
	if d.Log != nil {
		_, _ = fmt.Fprintf(d.Log, "Building the tools image %s. This happens once.\n", Image)
	}
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", Image, "-")
	cmd.Stdin = bytes.NewReader(dockerfile)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w\n%s", err, tail(out.String(), 20))
	}
	return nil
}

// ErrNoDocker means the docker command is not on PATH.
var ErrNoDocker = errors.New("docker is not on PATH")

// Run implements Runner.
func (d Docker) Run(ctx context.Context, input, outDir string, cmds []Cmd) error {
	inAbs, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	args := []string{"run", "--rm", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--mount", bindMount(inAbs, In, true), "--mount", bindMount(outAbs, Out, false)}
	if runtime.GOOS == "linux" {
		// Files in the output folder belong to the user, not to the image user.
		args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	args = append(args, Image, "sh", "-c", Script(cmds))
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tools image: %w: %s", err, tail(stderr.String(), 10))
	}
	return nil
}

// bindMount returns a --mount value. Docker reads it as CSV, so a quoted
// source keeps commas, colons and quotes in host paths.
func bindMount(source, target string, readOnly bool) string {
	m := `type=bind,"source=` + strings.ReplaceAll(source, `"`, `""`) + `",target=` + target
	if readOnly {
		m += ",readonly"
	}
	return m
}

// Script returns cmds as one shell script that stops at the first error.
func Script(cmds []Cmd) string {
	lines := []string{"set -e"}
	for _, c := range cmds {
		q := make([]string, len(c.Args))
		for i, a := range c.Args {
			q[i] = shellQuote(a)
		}
		line := strings.Join(q, " ")
		if c.Stdout != "" {
			line += " > " + shellQuote(Out+"/"+c.Stdout)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Missing returns the programs that are not on the host PATH.
func (Local) Missing() []string {
	var missing []string
	for _, p := range Programs {
		if _, err := exec.LookPath(p); err != nil {
			missing = append(missing, p)
		}
	}
	return missing
}

// Run implements Runner.
func (Local) Run(ctx context.Context, input, outDir string, cmds []Cmd) error {
	for _, c := range cmds {
		args := make([]string, len(c.Args))
		for i, a := range c.Args {
			args[i] = localPath(a, input, outDir)
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = outDir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if c.Stdout != "" {
			f, err := os.Create(filepath.Join(outDir, filepath.FromSlash(c.Stdout)))
			if err != nil {
				return err
			}
			cmd.Stdout = f
			err = cmd.Run()
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return fmt.Errorf("%s: %w: %s", args[0], err, tail(stderr.String(), 10))
			}
			continue
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w: %s", args[0], err, tail(stderr.String(), 10))
		}
	}
	return nil
}

// localPath maps the In and Out paths of a command to host paths.
func localPath(arg, input, outDir string) string {
	switch {
	case arg == In:
		return input
	case arg == Out:
		return outDir
	case strings.HasPrefix(arg, Out+"/"):
		return filepath.Join(outDir, filepath.FromSlash(strings.TrimPrefix(arg, Out+"/")))
	}
	return arg
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

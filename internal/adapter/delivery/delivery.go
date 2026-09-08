// Package delivery hands a brief to whoever is going to act on it (ADR 0046).
//
// Three adapters, and the default writes a file. An agent running in another
// terminal under a CLI msr has never heard of is the normal case, and a file
// works with all of them.
package delivery

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mondial7/mondspace-reviewer/internal/usecase/handoff"
)

// HandoffDir is where written briefs go, under the shared directory.
const HandoffDir = "handoff"

// File writes the brief to `<dir>/handoff/<batch>.md`.
//
// The default, and the only adapter with no dependency on anything: it is what
// makes a push work offline, in a repository whose agent msr does not know.
type File struct {
	// Dir is the shared directory — the same one the findings store is in.
	Dir string
	// Written is set to the path, so the command can tell somebody where to
	// look. It is a field rather than a return value because the interface is
	// the same for adapters that write nowhere.
	Written string
}

func (f *File) Name() string { return "file" }

func (f *File) Send(b handoff.Brief) error {
	dir := filepath.Join(f.Dir, HandoffDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, safeName(b.Batch)+".md")
	if err := os.WriteFile(path, []byte(handoff.Render(b)), 0o644); err != nil {
		return err
	}
	f.Written = path
	return nil
}

// Stdout writes the brief to a stream, for a pipe.
type Stdout struct{ W io.Writer }

func (s Stdout) Name() string { return "stdout" }

func (s Stdout) Send(b handoff.Brief) error {
	_, err := io.WriteString(s.W, handoff.Render(b))
	return err
}

// Clipboard puts the brief where a paste-into-terminal workflow expects it.
//
// The command is whatever the platform has. There is no fallback to writing a
// file: somebody who asked for the clipboard and got a file on disk would paste
// nothing and not know why.
type Clipboard struct {
	// Run is the exec hook, so a test can watch what would have been run.
	Run func(name string, args []string, stdin string) error
}

func (c Clipboard) Name() string { return "clipboard" }

func (c Clipboard) Send(b handoff.Brief) error {
	name, args := clipboardCommand()
	if name == "" {
		return fmt.Errorf("no clipboard command found (tried pbcopy, wl-copy, xclip) — try --to file")
	}
	run := c.Run
	if run == nil {
		run = runWithStdin
	}
	return run(name, args, handoff.Render(b))
}

// clipboardCommand is the first clipboard tool on this machine that exists.
func clipboardCommand() (string, []string) {
	candidates := [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	if runtime.GOOS == "darwin" {
		candidates = append([][]string{{"pbcopy"}}, candidates...)
	}
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return candidate[0], candidate[1:]
		}
	}
	return "", nil
}

func runWithStdin(name string, args []string, stdin string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

// Pick returns the adapter a reviewer asked for.
//
// An unknown name is an error rather than a fallback to the default: a push
// that silently went somewhere other than where it was sent is worse than one
// that did not go.
func Pick(name, dir string, stdout io.Writer) (handoff.Delivery, error) {
	switch name {
	case "", "file":
		return &File{Dir: dir}, nil
	case "stdout":
		return Stdout{W: stdout}, nil
	case "clipboard":
		return Clipboard{}, nil
	default:
		return nil, fmt.Errorf("unknown delivery %q — try file, stdout or clipboard", name)
	}
}

// safeName keeps a batch id a single, benign filename. Batch ids are minted by
// msr, but they also arrive from a flag.
func safeName(batch string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, batch)
	if cleaned == "" {
		return "batch"
	}
	return cleaned
}

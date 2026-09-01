// Package local runs the deterministic analysers that happen to be installed on
// this machine, over the files a review actually changed (ADR 0043).
//
// msr ships none of them and installs none of them. This detects what is on
// `PATH`, runs it under a wall-time cap, reads what it printed, and never lets
// any of that reach the review as a failure: analysis that cannot run is
// analysis that is not shown, and the settings page says which and why.
package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// Cap is how long one analyser may run before it is abandoned.
//
// The same contract the model has: analysis never blocks the review. A linter
// that has been thinking for half a minute about a two-file change is stuck or
// is analysing the whole repository, and either way the reviewer is not waiting
// for it.
const Cap = 30 * time.Second

// detectCap bounds the `--version` call. Generous for what it is, because it is
// paid once per tool per process and the cost of getting it wrong is a tool
// that is installed being reported as absent — which is worse than waiting.
// Several of these are shell wrappers that start a JVM or a Python interpreter
// to answer.
const detectCap = 8 * time.Second

// Scanner is the analysers this machine has, and what they have already said.
type Scanner struct {
	repoDir   string
	analysers []usecase.Analyser
	cap       time.Duration

	mu sync.Mutex
	// version is the tool's own version string, or empty for one that is not
	// installed. It is part of every cache key: a tool that was upgraded has
	// different opinions and its old answers are not reusable.
	version map[string]string
	// why is the detect failure for a tool that is not here, so the settings
	// page can say "not installed" rather than nothing at all.
	why map[string]string
	// broken is a tool that is installed and failed to run, reported once and
	// never again. A crashing analyser must not interrupt a review, and must
	// not interrupt it repeatedly.
	broken map[string]string
	// answers caches by (tool, version, argv, what every file it will see says).
	answers map[string][]domain.Reported
}

// New builds a scanner over a repository. Nothing is detected or run until Look
// is called: constructing this is free.
func New(repoDir string, analysers []usecase.Analyser) *Scanner {
	return &Scanner{
		repoDir: repoDir, analysers: analysers, cap: Cap,
		version: map[string]string{}, why: map[string]string{},
		broken: map[string]string{}, answers: map[string][]domain.Reported{},
	}
}

// WithCap overrides the wall-time cap, for tests and for a reviewer whose
// repository is slower than this assumes.
func (s *Scanner) WithCap(d time.Duration) *Scanner {
	if d > 0 {
		s.cap = d
	}
	return s
}

// Detect asks each analyser whether it is installed, once per process.
//
// A tool is present if its detect command exits zero. Not "a binary of that
// name exists": a broken install, a wrapper script pointing at a deleted
// virtualenv and a binary for the wrong architecture all pass a PATH lookup and
// all fail this.
func (s *Scanner) Detect(ctx context.Context) {
	for _, a := range s.analysers {
		s.mu.Lock()
		_, known := s.version[a.Name]
		_, ruled := s.why[a.Name]
		s.mu.Unlock()
		if known || ruled {
			continue
		}

		probe, cancel := context.WithTimeout(ctx, detectCap)
		out, err := s.run(probe, a.Detect)
		cancel()

		s.mu.Lock()
		if err != nil {
			s.why[a.Name] = "not installed"
		} else {
			// Bounded: `golangci-lint --version` is a build banner with a
			// commit and a timestamp in it, and the settings page has a column
			// for a version rather than a paragraph.
			s.version[a.Name] = usecase.Brief(firstLine(out), 44)
		}
		s.mu.Unlock()
	}
}

// Look runs every installed analyser that has an opinion about these files, and
// returns everything they said.
//
// prints is what each file currently contains, keyed by path — the caller
// already computes it for the incremental readings (ADR 0038), and it is what
// makes the cache correct: unchanged file, no run.
func (s *Scanner) Look(ctx context.Context, files []string, prints map[string]string,
	base string) []domain.Reported {

	s.Detect(ctx)

	var out []domain.Reported
	for _, a := range s.analysers {
		s.mu.Lock()
		version, installed := s.version[a.Name]
		s.mu.Unlock()
		if !installed {
			continue
		}

		argv, worth := a.Argv(files, s.repoDir, base)
		if !worth {
			continue
		}

		key := cacheKey(a, version, argv, files, prints)
		s.mu.Lock()
		cached, hit := s.answers[key]
		s.mu.Unlock()
		if hit {
			out = append(out, cached...)
			continue
		}

		run, cancel := context.WithTimeout(ctx, s.cap)
		output, err := s.run(run, argv)
		cancel()
		if err != nil && strings.TrimSpace(output) == "" {
			// A linter exits non-zero *because* it found something, so a
			// non-zero status with output is the ordinary success case. Nothing
			// at all is the failure.
			s.note(a.Name, err)
			continue
		}

		found := usecase.ReadFindings(a, output, s.repoDir)
		s.mu.Lock()
		s.answers[key] = found
		s.mu.Unlock()
		out = append(out, found...)
	}
	return out
}

// LookIn runs the same analysers over a copy of the code as it was before the
// change, so a finding can be asked the one question the cheap path cannot
// answer: were you here already? (ADR 0043)
//
// Uncached and unshared with Look. The whole point is that the content is
// different, and a cache keyed on file content would answer with the wrong
// side's findings if it were ever keyed slightly wrong. This is the expensive,
// on-demand path; it should be expensive rather than subtly wrong.
//
// Detection is shared, because whether gosec is installed is a fact about the
// machine and not about which copy of the code is being read.
func (s *Scanner) LookIn(ctx context.Context, dir string, files []string, base string) []domain.Reported {
	s.Detect(ctx)

	elsewhere := &Scanner{
		repoDir: dir, analysers: s.analysers, cap: s.cap,
		version: map[string]string{}, why: map[string]string{},
		broken: map[string]string{}, answers: map[string][]domain.Reported{},
	}
	s.mu.Lock()
	for name, version := range s.version {
		elsewhere.version[name] = version
	}
	s.mu.Unlock()

	// Files that did not exist before this change are not in the archive, and a
	// tool pointed at a path that is not there fails rather than reporting
	// nothing.
	here := make([]string, 0, len(files))
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			here = append(here, f)
		}
	}
	if len(here) == 0 {
		return nil
	}

	found := elsewhere.Look(ctx, here, nil, base)

	// A tool that broke over there is still a tool that broke, and the settings
	// page is the one place that is said.
	elsewhere.mu.Lock()
	broken := elsewhere.broken
	elsewhere.mu.Unlock()
	s.mu.Lock()
	for tool, why := range broken {
		if _, said := s.broken[tool]; !said {
			s.broken[tool] = why
		}
	}
	s.mu.Unlock()

	return found
}

// note records a tool that broke, once. Reported on the settings page and
// nowhere else: a review must never be interrupted by an analyser, and must
// especially never be interrupted by the same analyser every five seconds.
func (s *Scanner) note(tool string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, said := s.broken[tool]; said {
		return
	}
	s.broken[tool] = usecase.Brief(firstLine(err.Error()), 160)
}

// ToolStatus is one analyser as the settings page needs it.
type ToolStatus struct {
	Name string
	// Why is what this tool is for, from its definition.
	Why     string
	Present bool
	Version string
	// Absent is why it is not here, for a tool that was looked for and not
	// found. Naming what was *not* detected is half the point of the page: a
	// reviewer who thinks gosec is running and finds out here that it is not
	// has learnt something (ADR 0043).
	Absent string
	// Failed is why it broke, for a tool that is installed and did not work.
	Failed string
}

// Report is every analyser msr knows about and what became of it. Installed
// first, because that is the half a reviewer can act on.
func (s *Scanner) Report() []ToolStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]ToolStatus, 0, len(s.analysers))
	for _, a := range s.analysers {
		version, present := s.version[a.Name]
		out = append(out, ToolStatus{
			Name: a.Name, Why: a.Why, Present: present,
			Version: version, Absent: s.why[a.Name], Failed: s.broken[a.Name],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Present && !out[j].Present })
	return out
}

// Installed is how many analysers are actually available.
func (s *Scanner) Installed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.version)
}

// run executes one argv in the repository and returns everything it printed.
//
// stdout and stderr together, because tools disagree about which one a finding
// belongs on and several write the report to stderr while exiting zero.
func (s *Scanner) run(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("nothing to run")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.repoDir
	// An analyser is a program somebody else wrote, run over somebody's source.
	// It gets no stdin, and it gets the environment it needs to find its own
	// toolchain and nothing more is taken away from it.
	cmd.Stdin = nil
	cmd.Env = os.Environ()

	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	// Killing a process does not kill what it started, and a shell wrapper
	// whose child still holds the output pipe would keep Run waiting long past
	// the cap it was given — which is the cap not existing.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Errorf("gave up after %s", s.cap)
	}
	return out.String(), err
}

// cacheKey is what makes "unchanged file, no run" true.
//
// The tool, its version, the exact command, and what every file it is about to
// be shown currently says. A tool that was upgraded has different opinions; a
// command that changed is a different question; a file that moved is the only
// reason to ask again.
//
// One thing it deliberately does not cover: the tool's own configuration. A
// `.golangci.yml` edited while msr is running will not invalidate anything, and
// the answer to that is to restart — which is a smaller cost than stat'ing a
// guessed list of config filenames on every tick and still getting it wrong for
// the tool nobody thought of.
func cacheKey(a usecase.Analyser, version string, argv, files []string,
	prints map[string]string) string {

	h := sha256.New()
	fmt.Fprintln(h, a.Name, version, a.Format)
	fmt.Fprintln(h, strings.Join(argv, "\x00"))

	seen := make([]string, 0, len(files))
	for _, f := range files {
		if a.Applies(f) {
			seen = append(seen, f+"\x00"+prints[f])
		}
	}
	sort.Strings(seen)
	fmt.Fprintln(h, strings.Join(seen, "\n"))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

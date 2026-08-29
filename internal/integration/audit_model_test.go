//go:build model

// Package integration's model tests put msr's real prompts to a real model.
//
// The prompts are load-bearing code with no other test. Everything else in this
// repository is checked by something; a prompt edit that quietly stops catching
// a hardcoded secret would show up only as a security card that has gone quiet,
// which is indistinguishable from a change with nothing wrong in it.
//
// Run against a local llama-server (ADR 0019):
//
//	MSR_SUMMARIZER_URL=http://127.0.0.1:8081/v1 \
//	MSR_MODEL=qwen3-4b-instruct-2507 \
//	  go test -tags=model -timeout=20m ./internal/integration/...
package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/openai"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
)

// review builds a real repository, commits `before`, commits `after`, and
// returns the review msr would build from it.
//
// Real git output, not a hand-written approximation: a prompt has to be tested
// against exactly what production feeds it. The first version of this harness
// used a diff written by hand, and the `+` lines happened to come before the
// `-` lines — an arrangement git never produces — which was enough to change
// the answer.
func review(t *testing.T, before, after map[string]string) ([]domain.Unit, map[string]domain.Diff) {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet", ".")

	write := func(files map[string]string) {
		for path, body := range files {
			full := filepath.Join(dir, path)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	write(before)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "before")
	base := revParse(t, dir, "HEAD")

	write(after)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "--quiet", "-m", "after")
	head := revParse(t, dir, "HEAD")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	units, diffs, err := usecase.BuildFileUnits(ctx, gitsnap.New(dir, "t"), "t",
		domain.SnapshotRef{Commit: base}, domain.SnapshotRef{Commit: head},
		func(string) bool { return false })
	if err != nil {
		t.Fatalf("BuildFileUnits: %v", err)
	}
	return units, diffs
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

// seeded is a change with three security problems in it, each of a different
// kind, and two breaking changes. Written to be unambiguous rather than
// realistic: the point is to notice a prompt that has stopped working.
func seeded(t *testing.T) ([]domain.Unit, map[string]domain.Diff) {
	return review(t,
		map[string]string{
			"auth/token.go": `package auth

func Validate(token string) bool {
	return token != ""
}
`,
			"api/users.go": `package api

import "database/sql"

func FindUser(db *sql.DB, id string) (string, error) {
	var name string
	err := db.QueryRow("SELECT name FROM users WHERE id = $1", id).Scan(&name)
	return name, err
}
`,
		},
		map[string]string{
			"auth/token.go": `package auth

import "os"

// apiSecret signs every token.
const apiSecret = "sk-live-9f3a2b7c4d8e1f6a0b5c9d2e7f4a8b3c"

func Validate(token, scope string) bool {
	if os.Getenv("AUTH_DISABLED") == "1" {
		return true
	}
	return token != "" && scope != ""
}
`,
			"api/users.go": `package api

import (
	"database/sql"
	"fmt"
)

func FindUser(db *sql.DB, id string) (string, error) {
	var name string
	q := fmt.Sprintf("SELECT name FROM users WHERE id = '%s'", id)
	if err := db.QueryRow(q).Scan(&name); err != nil {
		return "", nil
	}
	return name, nil
}
`,
		})
}

// harmless is a change with nothing wrong in it. Half of what a prompt has to
// get right is staying quiet: one tuned until it never misses anything reports
// something every time, and a card that always has findings is one nobody
// reads.
func harmless(t *testing.T) ([]domain.Unit, map[string]domain.Diff) {
	return review(t,
		map[string]string{"README.md": "# demo\n"},
		map[string]string{
			"README.md": "# demo\n\nRun it with `go run .`.\n",
			"internal/text/wrap.go": `package text

import "strings"

// Wrap breaks a string into lines of at most n runes, on word boundaries.
func Wrap(s string, n int) []string {
	var out []string
	var line strings.Builder
	for _, word := range strings.Fields(s) {
		if line.Len() > 0 && line.Len()+1+len(word) > n {
			out = append(out, line.String())
			line.Reset()
		}
		if line.Len() > 0 {
			line.WriteString(" ")
		}
		line.WriteString(word)
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	return out
}
`,
		})
}

func modelUnderTest(t *testing.T) *openai.Summarizer {
	t.Helper()
	url := os.Getenv("MSR_SUMMARIZER_URL")
	model := os.Getenv("MSR_MODEL")
	if url == "" || model == "" {
		t.Skip("set MSR_SUMMARIZER_URL and MSR_MODEL to run the prompt tests")
	}
	return openai.New(url, model).WithAPIKey(os.Getenv("MSR_API_KEY"))
}

func TestTheSecurityPromptStillCatchesWhatItIsFor(t *testing.T) {
	sum := modelUnderTest(t)
	units, diffs := seeded(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	audit, _ := usecase.AuditFor(usecase.AuditSecurity)
	got, err := usecase.RunAudit(ctx, sum, audit, "seeded", units, diffs)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}

	all := strings.ToLower(got.Verdict)
	for _, f := range got.Findings {
		all += " " + strings.ToLower(f.File+" "+f.Note)
	}
	t.Logf("verdict: %s", got.Verdict)
	for _, f := range got.Findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.File, f.Note)
	}

	// Each seeded problem, recognised by any of the words a reasonable answer
	// would use. Matching on one exact phrasing would fail on wording rather
	// than on substance.
	for _, want := range []struct {
		what string
		any  []string
	}{
		{"the SQL injection", []string{"sql injection", "injection", "parameteri", "sprintf"}},
		{"the hardcoded secret", []string{"secret", "hardcoded", "hard-coded", "credential", "api key"}},
		{"the auth bypass", []string{"auth_disabled", "bypass", "environment variable"}},
	} {
		found := false
		for _, phrase := range want.any {
			if strings.Contains(all, phrase) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the security prompt no longer catches %s\nverdict: %s\nfindings: %+v",
				want.what, got.Verdict, got.Findings)
		}
	}

	// And it should weigh them: an injection and a committed secret are not
	// "worth knowing about".
	if got.Worst() != domain.SeverityHigh {
		t.Errorf("worst severity = %q, want high for an injection and a secret", got.Worst())
	}
}

func TestTheBreakingPromptStillCatchesASignatureChange(t *testing.T) {
	sum := modelUnderTest(t)
	units, diffs := seeded(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	audit, _ := usecase.AuditFor(usecase.AuditBreaking)
	got, err := usecase.RunAudit(ctx, sum, audit, "seeded", units, diffs)
	if err != nil {
		t.Fatalf("RunAudit: %v", err)
	}
	t.Logf("verdict: %s", got.Verdict)
	for _, f := range got.Findings {
		t.Logf("  [%s] %s — %s", f.Severity, f.File, f.Note)
	}

	all := strings.ToLower(got.Verdict)
	for _, f := range got.Findings {
		all += " " + strings.ToLower(f.File+" "+f.Note)
	}
	if !strings.Contains(all, "validate") && !strings.Contains(all, "signature") &&
		!strings.Contains(all, "scope") {
		t.Errorf("the breaking prompt no longer catches a changed exported signature:\n%+v", got)
	}
	if len(got.Findings) == 0 {
		t.Error("a changed exported signature is a breaking change")
	}
}

func TestBothPromptsStayQuietOnAHarmlessChange(t *testing.T) {
	// The half that is easy to lose. A prompt tuned until it never misses
	// anything reports something every time, and a card that always has
	// findings is a card nobody reads.
	sum := modelUnderTest(t)
	units, diffs := harmless(t)

	for _, kind := range []domain.AnalysisKind{usecase.AuditSecurity, usecase.AuditBreaking} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		audit, _ := usecase.AuditFor(kind)
		got, err := usecase.RunAudit(ctx, sum, audit, "harmless", units, diffs)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		t.Logf("%s verdict: %s", kind, got.Verdict)
		if len(got.Findings) != 0 {
			t.Errorf("%s invented %d findings on a word-wrap helper:\n%+v",
				kind, len(got.Findings), got.Findings)
		}
	}
}

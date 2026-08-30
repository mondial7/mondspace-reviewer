package claudecli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/claudecli"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
)

// fakeCLI writes a script that stands in for the real binary: it records the
// arguments and the prompt it was given, and prints whatever it was told to.
func fakeCLI(t *testing.T, stdout string, code int) (bin, log string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "claude")
	log = filepath.Join(dir, "call.log")

	script := "#!/bin/sh\n" +
		"{ echo \"ARGS: $@\"; echo '--- STDIN ---'; cat; } > " + log + "\n" +
		"cat <<'REPLY'\n" + stdout + "\nREPLY\n" +
		"exit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func TestThePromptGoesOnStdinNotTheCommandLine(t *testing.T) {
	// An audit prompt carries the diff and runs to several kilobytes. Argv is
	// the wrong place for it on any platform, and the wrong place for source
	// code on all of them.
	bin, log := fakeCLI(t, "hello", 0)

	got, err := claudecli.New(bin, "").Answer(context.Background(), "a question about\na diff", domain.AskContext{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if strings.TrimSpace(got) != "hello" {
		t.Errorf("Answer = %q", got)
	}

	call, _ := os.ReadFile(log)
	args, stdin, _ := strings.Cut(string(call), "--- STDIN ---")
	if strings.Contains(args, "a question about") {
		t.Errorf("the prompt should not be an argument:\n%s", args)
	}
	if !strings.Contains(stdin, "a question about") {
		t.Errorf("the prompt should arrive on stdin:\n%s", stdin)
	}
}

func TestItIsGivenNoTools(t *testing.T) {
	// The prompt already carries the change. A reviewer's model that can read
	// the filesystem is a different thing with different risks, and msr's whole
	// claim is that it only ever reads what it was given.
	bin, log := fakeCLI(t, "ok", 0)

	_, _ = claudecli.New(bin, "").Answer(context.Background(), "q", domain.AskContext{})

	call, _ := os.ReadFile(log)
	if !strings.Contains(string(call), "--allowed-tools") {
		t.Errorf("it should be run with no tools:\n%s", call)
	}
}

func TestAFencedReplyComesBackAsItsContents(t *testing.T) {
	// The CLI answers in prose around a fenced block, and often adds a
	// paragraph after it. Every caller then hunts for the first { and the last
	// }, which a sentence containing a brace would break.
	bin, _ := fakeCLI(t, "Here is the answer:\n\n```json\n{\"verdict\":\"clean\"}\n```\n\nAnd some words after it.", 0)

	got, err := claudecli.New(bin, "").Answer(context.Background(), "q", domain.AskContext{})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if strings.TrimSpace(got) != `{"verdict":"clean"}` {
		t.Errorf("Answer = %q, want just the fenced block", got)
	}
}

func TestAModelIsPassedThroughWhenOneIsAskedFor(t *testing.T) {
	bin, log := fakeCLI(t, "ok", 0)

	_, _ = claudecli.New(bin, "opus").Answer(context.Background(), "q", domain.AskContext{})

	if call, _ := os.ReadFile(log); !strings.Contains(string(call), "--model opus") {
		t.Errorf("the model should be passed on:\n%s", call)
	}
}

func TestAMissingBinaryIsAnHonestFailure(t *testing.T) {
	// Not a silent fallback: the reviewer chose this, and being quietly
	// answered by something else is worse than being told it is not there.
	_, err := claudecli.New(filepath.Join(t.TempDir(), "nope"), "").
		Answer(context.Background(), "q", domain.AskContext{})

	if err == nil {
		t.Fatal("want an error when the binary is not there")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("the error should name what is missing: %v", err)
	}
}

func TestAModelMeantForSomewhereElseIsNotPassedOn(t *testing.T) {
	// The model field is shared with the OpenAI-compatible engine, so it
	// usually holds the name of whatever is loaded in llama-server. Handing
	// that to the CLI fails the whole call — it is a name Claude has never
	// heard of — and the reviewer never asked for it.
	for _, name := range []string{"qwen3-4b-instruct-2507", "gemma-4-12b", ""} {
		bin, log := fakeCLI(t, "ok", 0)

		_, _ = claudecli.New(bin, name).Answer(context.Background(), "q", domain.AskContext{})

		if call, _ := os.ReadFile(log); strings.Contains(string(call), "--model") {
			t.Errorf("%q is not a Claude model and should not be passed:\n%s", name, call)
		}
	}
}

func TestAClaudeModelIsPassedOn(t *testing.T) {
	for _, name := range []string{"opus", "sonnet", "haiku", "claude-opus-4-6"} {
		bin, log := fakeCLI(t, "ok", 0)

		_, _ = claudecli.New(bin, name).Answer(context.Background(), "q", domain.AskContext{})

		if call, _ := os.ReadFile(log); !strings.Contains(string(call), "--model "+name) {
			t.Errorf("%q should be passed on:\n%s", name, call)
		}
	}
}

package contract_test

import (
	"go/format"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// The property the whole mechanism rests on: run the real formatter over a file
// and every fingerprint in it is unchanged. A reformatting that closed one
// finding and raised it again as new would make dismissals worthless in any
// repository with a formatter in its pre-commit hook.
func TestFingerprintSurvivesGofmt(t *testing.T) {
	ugly := `package p

func f(err error) error {
	if err  !=  nil    {
			return    err
	}
		return nil
}
`
	pretty, err := format.Source([]byte(ugly))
	if err != nil {
		t.Fatal(err)
	}
	if string(pretty) == ugly {
		t.Fatal("the fixture is already formatted, so this test proves nothing")
	}

	before := contract.FingerprintAt("internal/p/p.go", "errcheck", ugly, 4, 4)
	after := contract.FingerprintAt("internal/p/p.go", "errcheck", string(pretty), 4, 4)

	if before != after {
		t.Errorf("gofmt changed the fingerprint:\nbefore %s\nafter  %s", before, after)
	}
}

// A formatter that breaks one long line into three has changed nothing about
// what the code says. Newlines normalise to spaces so that it changes nothing
// about the fingerprint either.
func TestFingerprintSurvivesWrapping(t *testing.T) {
	oneLine := []string{`err := doTheThing(ctx, "a", "b", "c")`}
	wrapped := []string{
		"err := doTheThing(",
		`	ctx,`,
		`	"a", "b", "c",`,
		")",
	}

	if got, want := contract.Fingerprint("a.go", "r", wrapped), contract.Fingerprint("a.go", "r", oneLine); got != want {
		t.Errorf("wrapping changed the fingerprint:\nwrapped %s\none line %s", got, want)
	}
}

func TestFingerprintIsDeterministic(t *testing.T) {
	window := []string{"x := 1", "y := 2"}

	first := contract.Fingerprint("a.go", "rule", window)
	second := contract.Fingerprint("a.go", "rule", window)

	if first != second {
		t.Errorf("two runs disagreed: %s != %s", first, second)
	}
}

func TestFingerprintChangesWhenTheCodeDoes(t *testing.T) {
	was := contract.Fingerprint("a.go", "rule", []string{"return err"})
	now := contract.Fingerprint("a.go", "rule", []string{"return nil"})

	if was == now {
		t.Error("different code produced the same fingerprint")
	}
}

func TestFingerprintSeparatesRulesAndFiles(t *testing.T) {
	window := []string{"return err"}
	base := contract.Fingerprint("a.go", "one", window)

	if other := contract.Fingerprint("a.go", "two", window); other == base {
		t.Error("two rules on one line share a fingerprint")
	}
	if other := contract.Fingerprint("b.go", "one", window); other == base {
		t.Error("two files share a fingerprint")
	}
}

// Two tools reporting the same file differently are reporting the same file.
func TestFingerprintNormalisesThePath(t *testing.T) {
	window := []string{"return err"}
	want := contract.Fingerprint("internal/a.go", "r", window)

	for _, path := range []string{"./internal/a.go", `internal\a.go`, "internal/./a.go", "/internal/a.go"} {
		if got := contract.Fingerprint(path, "r", window); got != want {
			t.Errorf("Fingerprint(%q) = %s, want the same as internal/a.go (%s)", path, got, want)
		}
	}
}

// A finding about the file as a whole has no window, and is still identifiable.
func TestFingerprintOfAWholeFileItem(t *testing.T) {
	first := contract.FingerprintAt("a.go", "no-test", "anything\nat all\n", 0, 0)
	second := contract.FingerprintAt("a.go", "no-test", "the file has since changed entirely", 0, 0)

	if first != second {
		t.Error("a file-wide finding moved when the file's contents changed")
	}
	if first == contract.FingerprintAt("b.go", "no-test", "", 0, 0) {
		t.Error("a file-wide finding does not distinguish files")
	}
}

func TestWindowClampsToTheFile(t *testing.T) {
	lines := []string{"one", "two", "three"}

	if got := contract.Window(lines, 1, 1); len(got) != 3 {
		t.Errorf("Window at the top = %v, want the whole short file", got)
	}
	if got := contract.Window(lines, 3, 3); len(got) != 3 {
		t.Errorf("Window at the bottom = %v, want the whole short file", got)
	}
	if got := contract.Window(lines, 99, 99); got != nil {
		t.Errorf("Window past the end = %v, want nothing", got)
	}
}

func TestWindowIsBoundedByTheRadius(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}

	got := contract.Window(lines, 50, 50)

	if want := 2*contract.WindowRadius + 1; len(got) != want {
		t.Errorf("Window = %d lines, want %d", len(got), want)
	}
}

// An edit outside the window is somebody else's change, and must not close this
// item and raise it again.
func TestFingerprintIgnoresDistantEdits(t *testing.T) {
	// The edit starts well outside the window on either side of line 21.
	body := strings.Repeat("padding\n", 20) + "return err\n" + strings.Repeat("padding\n", 20)
	edited := strings.Repeat("rewritten\n", 15) + strings.Repeat("padding\n", 5) +
		"return err\n" + strings.Repeat("padding\n", 5) + strings.Repeat("rewritten\n", 15)

	if a, b := contract.FingerprintAt("a.go", "r", body, 21, 21), contract.FingerprintAt("a.go", "r", edited, 21, 21); a != b {
		t.Error("an edit twenty lines away changed the fingerprint")
	}
}

package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
)

// WindowRadius is how many lines either side of an item go into its
// fingerprint.
//
// Small on purpose. The window exists to tell two findings of the same rule in
// the same file apart, not to describe the function they are in — and every
// line included is another line whose editing costs a dismissal. Five is enough
// to separate the repeated shapes that actually occur (a run of similar error
// checks, a table of cases) and short enough that a comment rewritten a dozen
// lines away is not this item's problem.
const WindowRadius = 5

// Fingerprint is an item's identity across runs (ADR 0044, ADR 0045).
//
// Line numbers cannot be identity: a diff grows above the thing you are looking
// at constantly. What is stable is where the file is, which rule fired, and
// what the code around it says — so those are what it is made of.
//
// Reformatting must not change it, and that is the property the tests are
// about: whitespace is collapsed before hashing, so indentation, alignment and
// a line that was wrapped into three all hash the same.
//
// Comments are *not* stripped. Doing it properly needs a lexer per language,
// and doing it approximately is worse than not doing it: `#` opens a comment in
// Python and a directive in C, and a normaliser that is wrong silently changes
// identity, which is the one failure this whole mechanism exists to prevent.
func Fingerprint(filePath, ruleID string, window []string) string {
	sum := sha256.Sum256([]byte(
		NormalisePath(filePath) + "\x00" +
			ruleID + "\x00" +
			normaliseWindow(window),
	))
	return hex.EncodeToString(sum[:])
}

// NormalisePath puts a path in the one form the store uses: repository-relative,
// forward slashes, no leading "./".
//
// Two tools reporting the same file as `./internal/a.go` and `internal/a.go`
// are reporting the same file, and a fingerprint that disagreed would raise
// every finding twice on a machine where both are installed.
func NormalisePath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	return strings.TrimPrefix(p, "/")
}

// normaliseWindow reduces the code around an item to the part reformatting
// cannot change: every space, tab and newline is removed, and a comma left in
// front of a closing bracket goes with them.
//
// Removing whitespace rather than collapsing it is what makes this survive a
// real formatter. Indentation, alignment, a space after a comma and a long call
// broken across four lines are all whitespace; the trailing comma the formatter
// adds when it breaks that call is the one thing that is not, so it is handled
// beside them.
//
// Two costs, both accepted. `int x` and `intx` normalise alike — which matters
// only if one rule fires on both, in one file, within eleven lines. And in a
// language where indentation is syntax, two blocks that differ only in depth
// look the same here; the same eleven-line, one-rule proviso applies, and the
// alternative is a fingerprint that moves every time somebody re-indents a
// Python file, which is the failure this exists to prevent.
var closers = strings.NewReplacer(",)", ")", ",]", "]", ",}", "}")

func normaliseWindow(window []string) string {
	var b strings.Builder
	for _, line := range window {
		for _, r := range line {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' {
				continue
			}
			b.WriteRune(r)
		}
	}
	return closers.Replace(b.String())
}

// Window is the lines of a file that go into a fingerprint for an item spanning
// start..end, 1-indexed and inclusive.
//
// A start of zero means the item is about the file as a whole — some tools do
// report that, and plenty of human notes are — and then there is no window at
// all: the path and the rule are the identity, which is exactly right for a
// finding that is not about any particular line.
//
// Out-of-range lines are clamped rather than refused. A file that has been
// edited since the tool ran is the normal case, not an error.
func Window(lines []string, start, end int) []string {
	if start <= 0 {
		return nil
	}
	if end < start {
		end = start
	}

	from := start - 1 - WindowRadius
	if from < 0 {
		from = 0
	}
	to := end + WindowRadius
	if to > len(lines) {
		to = len(lines)
	}
	if from >= to {
		return nil
	}
	return lines[from:to]
}

// FingerprintAt is the whole job in one call: split the file, take the window,
// hash it. Callers that already hold the lines should use Window and
// Fingerprint directly rather than re-splitting a file per finding.
func FingerprintAt(filePath, ruleID, fileBody string, start, end int) string {
	return Fingerprint(filePath, ruleID, Window(strings.Split(fileBody, "\n"), start, end))
}

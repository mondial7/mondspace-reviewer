package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// askAboveRepos is how many discovered repositories are opened without asking.
// A handful is not a decision worth interrupting a launch for; forty is, and
// each one costs a git scan and a row in every list.
const askAboveRepos = 5

// selectRepos decides which discovered repositories to open. Up to
// askAboveRepos it opens them all silently. Beyond that it prints the list and
// requires a choice, because quietly opening forty checkouts is not a default
// anyone would have picked.
//
// Accepts "1,3,5", "2-4", a mix of the two, or "all".
func selectRepos(repos []string, in io.Reader, out io.Writer, interactive bool) ([]string, error) {
	if len(repos) <= askAboveRepos {
		return repos, nil
	}
	if !interactive {
		// A script or CI run has nobody to ask, and guessing here would be worse
		// than stopping: say what was found and how to choose.
		return nil, fmt.Errorf("found %d repositories under this directory; "+
			"choose them explicitly with --repo=<path> (repeatable)", len(repos))
	}

	fmt.Fprintf(out, "\nFound %d repositories:\n\n", len(repos))
	for i, r := range repos {
		fmt.Fprintf(out, "  %d) %s\n", i+1, filepath.Base(r))
	}
	fmt.Fprintf(out, "\nWhich to open? (e.g. 1,3,5 or 2-4 or all): ")

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return nil, fmt.Errorf("no selection made")
	}

	picked, err := parseSelection(strings.TrimSpace(line), len(repos))
	if err != nil {
		return nil, err
	}

	chosen := make([]string, 0, len(picked))
	for _, i := range picked {
		chosen = append(chosen, repos[i])
	}
	return chosen, nil
}

// parseSelection turns "1,3,5", "2-4" or "all" into zero-based indexes, in the
// order the list was shown and without duplicates.
func parseSelection(input string, n int) ([]int, error) {
	if strings.EqualFold(input, "all") {
		all := make([]int, n)
		for i := range all {
			all[i] = i
		}
		return all, nil
	}
	if input == "" {
		return nil, fmt.Errorf("no repositories chosen")
	}

	seen := map[int]bool{}
	var picked []int
	add := func(i int) error {
		if i < 1 || i > n {
			return fmt.Errorf("%d is not one of the %d repositories listed", i, n)
		}
		if !seen[i-1] {
			seen[i-1] = true
			picked = append(picked, i-1)
		}
		return nil
	}

	for _, part := range strings.Split(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			from, err1 := strconv.Atoi(strings.TrimSpace(lo))
			to, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("could not read the range %q", part)
			}
			for i := from; i <= to; i++ {
				if err := add(i); err != nil {
					return nil, err
				}
			}
			continue
		}
		i, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("could not read %q as a number", part)
		}
		if err := add(i); err != nil {
			return nil, err
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("no repositories chosen")
	}
	return picked, nil
}

// onTerminal reports whether stdin is something a person is typing into, so the
// launch prompt is never shown to a script that cannot answer it.
func onTerminal() bool { return isTerminal(os.Stdin) }

// isTerminal is the character-device test, plus the case it gets wrong on its
// own: /dev/null is a character device too, and redirecting from it is exactly
// how a script says "I have no input to give you".
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

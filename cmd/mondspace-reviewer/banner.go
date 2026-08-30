package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// The wordmark. Plain ASCII rather than box-drawing: this has to survive a
// terminal with no fancy font, an ssh session through something old, and a
// screenshot in a bug report.
//
// The rows are the same width by construction, and a test says so — a wordmark
// with a row a character short is the kind of thing nothing else notices.
var wordmark = [...]string{
	` __  __ ___ ___ `,
	`|  \/  / __| _ \`,
	`| |\/| \__ \   /`,
	`|_|  |_|___/_|_\`,
}

// Hung off the side, so the whole greeting is four lines rather than seven.
func asides(version string) [len(wordmark)]string {
	return [len(wordmark)]string{
		"",
		"mondspace-reviewer " + version,
		"review what your coding agent actually did",
		"",
	}
}

// The accent from the web app, as close as 256 colours get.
const (
	tint  = "\x1b[38;5;141m"
	faint = "\x1b[2m"
	reset = "\x1b[0m"
)

// banner writes the greeting. It is never given stdout: stdout is somebody's
// pipe, and this is decoration.
func banner(w io.Writer, version string, colour bool) {
	side := asides(version)
	for i, row := range wordmark {
		art, aside := row, side[i]
		if colour {
			art = tint + art + reset
			if aside != "" {
				aside = faint + aside + reset
			}
		}
		if aside == "" {
			fmt.Fprintln(w, art)
			continue
		}
		fmt.Fprintf(w, "%s   %s\n", art, aside)
	}
	fmt.Fprintln(w)
}

// greets reports whether a command is one a person is watching.
//
// The others are read by something else: `ingest` is a hook that must stay
// silent, `mcp` speaks a protocol on stdout and its stderr is a client's log,
// and `export` and `version` are answers somebody is piping somewhere. Four
// lines of ASCII art in the middle of any of those is a bug, not a flourish.
func greets(command string) bool {
	switch command {
	case "web", "review", "ask", "gc", "install-hooks", "help", "--help", "-h":
		return true
	}
	return false
}

// wantsBanner decides whether to draw it at all.
func wantsBanner(command string, terminal bool) bool {
	if os.Getenv("MSR_NO_BANNER") != "" {
		return false
	}
	return terminal && greets(command)
}

// greet draws the banner for a command, if anything is watching. It reuses the
// terminal test the repository picker already had, /dev/null case and all.
func greet(command string) {
	if os.Stderr == nil || !wantsBanner(command, isTerminal(os.Stderr)) {
		return
	}
	banner(os.Stderr, strings.TrimPrefix(released(), "v"), true)
}

package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// version is stamped by goreleaser at build time; a `go build` leaves it empty
// and the module's own build info answers instead.
var version = ""

// command is one thing msr can do, and the one-line summary help shows for it.
// Keeping the list here rather than in a printf means `help` cannot drift from
// what the dispatcher accepts — the test walks this table.
var commands = []struct {
	name    string
	summary string
	usage   string
	flags   []string
}{
	{
		"web", "Review in the browser — the cockpit, and the primary interface.",
		"msr web [--repo=<path> …] [--session=<id>] [--addr=127.0.0.1:7777]",
		[]string{
			"--repo=<path>    repository to review; repeatable. With none given, msr",
			"                 opens the checkout it is in, or offers the checkouts",
			"                 one level below it.",
			"--session=<id>   which review to open; the newest if omitted",
			"--addr=<addr>    where to listen (localhost only by default)",
			"--out=<dir>      store root, per repository (default .mondspace-reviewer)",
			"--pg-schema=<s>  Postgres schema when MSR_POSTGRES_DSN is set",
			"--summarizer-url, --model   the OpenAI-compatible endpoint to narrate with",
		},
	},
	{
		"review", "Review in the terminal. --plain is scriptable; --tui is unmaintained.",
		"msr review [--tui|--plain] [--source=replay|hooks|opencode] [--session=<id>]",
		[]string{
			"--plain          line-oriented output, scriptable",
			"--tui            the original terminal queue — unmaintained, use `msr web`",
			"--source=<src>   replay a file, tail Claude Code hooks, or tail OpenCode",
			"--since=<ref>    review a commit range instead of a session",
			"--until=<ref>    the far end of that range (default: the working tree)",
			"--verbose, -v    list each unit's member events and snapshot refs",
		},
	},
	{
		"ask", "Ask a question answered only from the log, diffs and your notes.",
		`msr ask [--scope=session|unit] --session=<id> "did the retry have a stated reason?"`,
		[]string{"--scope=<s>      session (default) or unit", "--unit=<id>      which unit, for --scope=unit"},
	},
	{
		"export", "Write the review up: markdown, JSON, or one Slack message.",
		"msr export --format=md|json|slack --session=<id>",
		[]string{"--format=<f>     md (default), json, or slack"},
	},
	{
		"ingest", "Append one agent event. Reads hook JSON on stdin and always exits 0.",
		"msr ingest --kind=<kind> < event.json",
		[]string{"--kind=<k>       which hook fired"},
	},
	{
		"install-hooks", "Write the agent hooks into .claude/settings.json (merges, never clobbers).",
		"msr install-hooks --dir=.",
		[]string{"--dir=<path>     project to install into", "--command=<path> the msr binary the hooks should call"},
	},
	{
		"mcp", "Serve the review to a coding agent over MCP, on stdin/stdout.",
		"msr mcp [--out=.mondspace-reviewer]",
		[]string{
			"--out=<dir>      store root to read (default .mondspace-reviewer)",
			"",
			"Read-only, and it reads the store rather than the code: no git, no",
			"model, no network. The agent pulls when it wants to — msr never",
			"interrupts it.",
			"",
			"What the human wrote is served by review_status, review_feedback and",
			"review_file. What a model inferred is behind model_findings, which",
			"says on every reply that it needs checking. workspace_feedback and",
			"workspace_search read every review and say so.",
			"",
			"Point your agent's MCP client at it, e.g. in .mcp.json:",
			`  {"mcpServers":{"msr":{"command":"msr","args":["mcp"]}}}`,
		},
	},
	{
		"gc", "Delete the throwaway review refs left under refs/mondspace/review/.",
		"msr gc [--session=<id>] [--repo=.] [--dry-run]",
		[]string{"--dry-run        print what would be removed, remove nothing"},
	},
	{"version", "Print the version.", "msr version", nil},
	{"help", "Show this, or the flags for one command.", "msr help [command]", nil},
}

// runHelp prints the whole list, or one command's flags.
func runHelp(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		for _, c := range commands {
			if c.name == args[0] {
				fmt.Fprintf(stdout, "%s\n\n  %s\n\n", c.summary, c.usage)
				for _, f := range c.flags {
					fmt.Fprintf(stdout, "  %s\n", f)
				}
				fmt.Fprintln(stdout)
				return nil
			}
		}
		return fmt.Errorf("unknown command %q — try `msr help`", args[0])
	}

	fmt.Fprint(stdout, `msr — a review companion for autonomous coding agents.

It watches an agent work and turns what it did into a review you can read: the
session as a story, beside the real diffs. It never writes to the agent.

USAGE

  msr <command> [flags]

COMMANDS

`)
	width := 0
	for _, c := range commands {
		if len(c.name) > width {
			width = len(c.name)
		}
	}
	for _, c := range commands {
		fmt.Fprintf(stdout, "  %-*s  %s\n", width, c.name, c.summary)
	}

	fmt.Fprintf(stdout, `
GETTING STARTED

  msr install-hooks --dir=.     let msr see what your agent does
  msr web                       review it in the browser

  Try it with no agent, terminal or network:
  msr review --source=replay --file=testdata/sessions/basic.jsonl --plain

  msr help <command>            flags for one command

ENVIRONMENT

  MSR_API_KEY         bearer token for an authenticated summarizer endpoint
  MSR_POSTGRES_DSN    use PostgreSQL instead of the JSONL store
  MSR_NO_THINKING=1   ask the model to skip its reasoning phase

Docs: https://github.com/mondial7/mondspace-reviewer#readme
`)
	return nil
}

// runVersion reports the build. goreleaser stamps `version`; a plain `go build`
// or `go install` falls back to the module's own build info.
func runVersion(stdout io.Writer) error {
	fmt.Fprintln(stdout, "msr "+strings.TrimPrefix(released(), "v"))
	return nil
}

// released is the version this binary claims to be.
func released() string {
	info, _ := debug.ReadBuildInfo()
	return describeVersion(version, info)
}

// describeVersion turns what the build knows about itself into something worth
// reading.
//
// goreleaser stamps the tag, and that is the whole answer for a release. For a
// local build Go offers its own version instead, and here that is a trap: the
// module path carries no /v6, so Go reports a v1 pseudo-version — a v6 binary
// announcing itself as 1.0.1-0.20260830083531-f2fafe9fda48+dirty, which names
// the right commit in the least readable way available and the wrong major
// version outright.
//
// The commit is the useful part, so that is what a development build says.
func describeVersion(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}
	if info == nil {
		return "dev"
	}

	var revision string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if dirty {
		// Worth saying out loud: it is the commit plus whatever is not in it.
		return "dev " + revision + " (dirty)"
	}
	return "dev " + revision
}

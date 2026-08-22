// Command mondspace-reviewer (msr) is a terminal review companion for an
// autonomous coding agent. M0 exposes the walking skeleton: replay a recorded
// log, cluster it, store it, and print the units with the plain presenter.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/marcomondini/mondspace-reviewer/internal/adapter/presenter/plain"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/source/replay"
	"github.com/marcomondini/mondspace-reviewer/internal/adapter/store/jsonl"
	"github.com/marcomondini/mondspace-reviewer/internal/usecase"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "msr:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "review" {
		return fmt.Errorf("usage: msr review --source=replay --file=<path> --plain [--out=<dir>]")
	}

	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	source := fs.String("source", "replay", "event source (replay)")
	file := fs.String("file", "", "recorded log to replay")
	usePlain := fs.Bool("plain", false, "use the plain presenter")
	out := fs.String("out", ".mondspace-reviewer", "store root directory")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *source != "replay" {
		return fmt.Errorf("unknown source %q (M0 supports replay)", *source)
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	if !*usePlain {
		return fmt.Errorf("--plain is required (M0 has no TUI)")
	}

	src := replay.New(*file)
	store := jsonl.New(*out)
	pres := plain.New(stdout)

	return usecase.Review(context.Background(), src, store, pres)
}

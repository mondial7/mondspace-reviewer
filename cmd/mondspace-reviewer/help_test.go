package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestHelpListsEveryCommand(t *testing.T) {
	var out bytes.Buffer

	if err := run(context.Background(), []string{"help"}, nil, &out); err != nil {
		t.Fatalf("help: %v", err)
	}
	got := out.String()

	// Every command the dispatcher accepts must be documented, or help is a
	// list that quietly rots as commands are added.
	for _, cmd := range []string{"web", "review", "ask", "export", "ingest", "install-hooks", "gc", "version"} {
		if !strings.Contains(got, cmd) {
			t.Errorf("help does not mention %q:\n%s", cmd, got)
		}
	}
	// It should be usable on its own: how to start, and where to read more.
	if !strings.Contains(got, "msr web") {
		t.Errorf("help should show how to start:\n%s", got)
	}
}

func TestHelpIsReachableTheUsualWays(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {}} {
		var out bytes.Buffer
		err := run(context.Background(), args, nil, &out)
		if err != nil {
			t.Errorf("run(%v) = %v, want help rather than an error", args, err)
		}
		if !strings.Contains(out.String(), "msr web") {
			t.Errorf("run(%v) printed no help:\n%s", args, out.String())
		}
	}
}

func TestHelpForOneCommandShowsItsFlags(t *testing.T) {
	var out bytes.Buffer

	if err := run(context.Background(), []string{"help", "web"}, nil, &out); err != nil {
		t.Fatalf("help web: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "--repo") || !strings.Contains(got, "--session") {
		t.Errorf("help web should list its flags:\n%s", got)
	}
}

func TestUnknownCommandPointsAtHelp(t *testing.T) {
	var out bytes.Buffer

	err := run(context.Background(), []string{"frobnicate"}, nil, &out)

	if err == nil {
		t.Fatal("an unknown command should be an error")
	}
	if !strings.Contains(err.Error(), "msr help") {
		t.Errorf("the error should point at help, got: %v", err)
	}
}

func TestVersionPrintsSomething(t *testing.T) {
	var out bytes.Buffer

	if err := run(context.Background(), []string{"version"}, nil, &out); err != nil {
		t.Fatalf("version: %v", err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("version printed nothing")
	}
}

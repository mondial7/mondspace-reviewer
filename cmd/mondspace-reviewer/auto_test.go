package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoIsOffUntilItIsTurnedOn(t *testing.T) {
	repo, shared := repoWithAFinding(t)

	if got := msr(t, "auto", "status", "--repo="+repo, "--dir="+shared); !strings.Contains(got, "auto-mode off") {
		t.Errorf("auto-mode is not off by default:\n%s", got)
	}

	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	got := msr(t, "auto", "run", "--repo="+repo, "--dir="+shared)

	if !strings.Contains(got, "auto-mode is off") {
		t.Errorf("a run with auto-mode off said %q", got)
	}
	if _, err := os.Stat(filepath.Join(shared, "handoff")); !os.IsNotExist(err) {
		t.Error("a run with auto-mode off delivered a brief")
	}
}

// With auto-mode disabled, no code path reaches an implementation agent
// without an explicit human command — including the scan that found it.
func TestNothingIsDeliveredByAScan(t *testing.T) {
	repo, shared := repoWithAFinding(t)

	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)

	if _, err := os.Stat(filepath.Join(shared, "handoff")); !os.IsNotExist(err) {
		t.Error("a scan delivered something to an agent")
	}
}

func TestAutoRunPushesWhenItIsOn(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	msr(t, "auto", "on", "--repo="+repo, "--dir="+shared)

	// The default floor is high; this finding is medium, so it is held back
	// with a reason rather than silently ignored.
	got := msr(t, "auto", "run", "--repo="+repo, "--dir="+shared)
	if !strings.Contains(got, "nothing pushed") {
		t.Errorf("the default policy pushed a medium finding: %q", got)
	}
	log, err := os.ReadFile(filepath.Join(shared, "judge.jsonl"))
	if err != nil {
		t.Fatalf("no decision log: %v", err)
	}
	if !strings.Contains(string(log), "below the severity floor") {
		t.Errorf("the log does not say why it was held back:\n%s", log)
	}

	// Lower the floor and it goes.
	write(t, shared, "auto.toml", "[auto]\n  enabled = true\n  min_severity = \"low\"\n  cooldown = \"0s\"\n")
	if got := msr(t, "auto", "run", "--repo="+repo, "--dir="+shared); !strings.Contains(got, "the judge pushed 1 item") {
		t.Errorf("the judge did not push: %q", got)
	}
	if listing := msr(t, "findings", "--repo="+repo, "--dir="+shared); !strings.Contains(listing, "pushed") {
		t.Errorf("the item is not recorded as pushed:\n%s", listing)
	}
}

// Every decision, accepted or rejected, appears in judge.jsonl with a reason.
func TestEveryJudgeDecisionIsLogged(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	write(t, shared, "auto.toml", "[auto]\n  enabled = true\n  min_severity = \"low\"\n  cooldown = \"0s\"\n")

	msr(t, "auto", "run", "--repo="+repo, "--dir="+shared)
	log, err := os.ReadFile(filepath.Join(shared, "judge.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(log), `"chosen":true`) || !strings.Contains(string(log), `"reason"`) {
		t.Errorf("the decision log is missing the decision or its reason:\n%s", log)
	}
	if got := msr(t, "auto", "status", "--repo="+repo, "--dir="+shared); !strings.Contains(got, "1 chosen") {
		t.Errorf("status does not count the decisions:\n%s", got)
	}
}

// msr auto off takes effect before the next checkpoint.
func TestAutoOffStopsTheNextRun(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	write(t, shared, "auto.toml", "[auto]\n  enabled = true\n  min_severity = \"low\"\n  cooldown = \"0s\"\n")

	msr(t, "auto", "off", "--repo="+repo, "--dir="+shared)
	got := msr(t, "auto", "run", "--repo="+repo, "--dir="+shared)

	if !strings.Contains(got, "auto-mode is off") {
		t.Errorf("a run after `auto off` said %q", got)
	}
}

// Any manual push suspends auto-mode for the remainder of the session.
func TestAManualPushSuspendsAutoMode(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	write(t, shared, "auto.toml", "[auto]\n  enabled = true\n  min_severity = \"low\"\n  cooldown = \"0s\"\n")
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))

	msr(t, "push", id, "--repo="+repo, "--dir="+shared)
	got := msr(t, "auto", "run", "--repo="+repo, "--dir="+shared)

	if !strings.Contains(got, "suspended") {
		t.Errorf("auto-mode was not suspended by a manual push: %q", got)
	}
	if status := msr(t, "auto", "status", "--repo="+repo, "--dir="+shared); !strings.Contains(status, "suspended") {
		t.Errorf("status does not mention the suspension:\n%s", status)
	}
}

// Turning it back on is the explicit act that lifts the suspension.
func TestAutoOnClearsTheSuspension(t *testing.T) {
	repo, shared := repoWithAFinding(t)
	msr(t, "scan", "--repo="+repo, "--since=start", "--dir="+shared)
	id := firstID(t, msr(t, "findings", "--repo="+repo, "--dir="+shared))
	msr(t, "push", id, "--repo="+repo, "--dir="+shared)

	msr(t, "auto", "on", "--repo="+repo, "--dir="+shared)

	if status := msr(t, "auto", "status", "--repo="+repo, "--dir="+shared); strings.Contains(status, "suspended") {
		t.Errorf("the suspension survived `auto on`:\n%s", status)
	}
}

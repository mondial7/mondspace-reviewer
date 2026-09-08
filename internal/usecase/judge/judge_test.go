package judge_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/judge"
)

var now = time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)

func finding(id string, sev contract.Severity) contract.Item {
	return contract.Item{
		ID: id, Fingerprint: "fp-" + id,
		Source: contract.SourceAnalyser, Producer: "gosec",
		Location: contract.Location{Path: id + ".go", StartLine: 1},
		Severity: sev, Directive: "do the thing",
	}
}

func on() judge.Policy {
	policy := judge.DefaultPolicy()
	policy.Enabled = true
	return policy
}

// Off is off, and it is the default.
func TestOffByDefault(t *testing.T) {
	verdict := judge.Decide([]contract.Item{finding("a", contract.SeverityHigh)}, judge.State{}, judge.DefaultPolicy(), now)

	if len(verdict.Chosen) != 0 {
		t.Errorf("auto-mode pushed with the default policy: %+v", verdict.Chosen)
	}
	if verdict.Held == "" {
		t.Error("nothing said why it held back")
	}
}

func TestChoosesTheWorstFirst(t *testing.T) {
	verdict := judge.Decide([]contract.Item{
		finding("low", contract.SeverityLow),
		finding("high", contract.SeverityHigh),
	}, judge.State{}, on(), now)

	if len(verdict.Chosen) != 1 || verdict.Chosen[0].ID != "high" {
		t.Fatalf("chose %+v, want only the high finding", verdict.Chosen)
	}
}

// Every decision appears, accepted or rejected, with a reason. If you cannot
// see why a finding was held back you cannot trust the ones it let through.
func TestEveryDecisionIsRecordedWithAReason(t *testing.T) {
	dismissed := finding("dismissed", contract.SeverityHigh)
	dismissed.Verdict = contract.VerdictDismissed
	inFlight := finding("in-flight", contract.SeverityHigh)
	inFlight.State = contract.StatePushed
	silent := finding("no-directive", contract.SeverityHigh)
	silent.Directive = ""

	verdict := judge.Decide([]contract.Item{
		finding("high", contract.SeverityHigh), finding("low", contract.SeverityLow), dismissed, inFlight, silent,
	}, judge.State{}, on(), now)

	if len(verdict.Decisions) != 5 {
		t.Fatalf("logged %d decisions for five findings", len(verdict.Decisions))
	}
	reasons := map[string]string{}
	for _, decision := range verdict.Decisions {
		if decision.Reason == "" {
			t.Errorf("%s was decided with no reason", decision.ItemID)
		}
		reasons[decision.ItemID] = decision.Reason
	}
	for id, want := range map[string]string{
		"low":          "below the severity floor",
		"dismissed":    "dismissed",
		"in-flight":    "already in flight",
		"no-directive": "no directive",
	} {
		if !strings.Contains(reasons[id], want) {
			t.Errorf("%s was held back because %q, want something about %q", id, reasons[id], want)
		}
	}
}

// The judge may select, order and phrase. It may never author.
func TestOnlyEverReturnsWhatItWasGiven(t *testing.T) {
	given := []contract.Item{finding("a", contract.SeverityHigh), finding("b", contract.SeverityHigh)}

	verdict := judge.Decide(given, judge.State{}, on(), now)

	known := map[string]bool{"a": true, "b": true}
	for _, item := range verdict.Chosen {
		if !known[item.ID] {
			t.Errorf("the judge produced an item nobody gave it: %+v", item)
		}
	}
}

func TestBoundedByTheBatchSize(t *testing.T) {
	var many []contract.Item
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, finding(id, contract.SeverityHigh))
	}

	verdict := judge.Decide(many, judge.State{}, on(), now)

	if len(verdict.Chosen) != judge.DefaultPolicy().MaxPerBatch {
		t.Errorf("chose %d items, want the batch cap of %d", len(verdict.Chosen), judge.DefaultPolicy().MaxPerBatch)
	}
}

func TestBoundedByTheSessionBudget(t *testing.T) {
	spent := judge.State{Pushes: judge.DefaultPolicy().MaxPushes}

	verdict := judge.Decide([]contract.Item{finding("a", contract.SeverityHigh)}, spent, on(), now)

	if len(verdict.Chosen) != 0 || !strings.Contains(verdict.Held, "budget") {
		t.Errorf("pushed past the session budget: %+v, held %q", verdict.Chosen, verdict.Held)
	}
}

func TestBoundedByTheCooldown(t *testing.T) {
	justPushed := judge.State{LastPush: now.Add(-time.Minute)}

	verdict := judge.Decide([]contract.Item{finding("a", contract.SeverityHigh)}, justPushed, on(), now)

	if len(verdict.Chosen) != 0 || !strings.Contains(verdict.Held, "cooldown") {
		t.Errorf("pushed inside the cooldown: %+v, held %q", verdict.Chosen, verdict.Held)
	}
	// And it comes back once the cooldown has passed.
	later := judge.Decide([]contract.Item{finding("a", contract.SeverityHigh)}, justPushed, on(), now.Add(10*time.Minute))
	if len(later.Chosen) != 1 {
		t.Errorf("still held after the cooldown: %q", later.Held)
	}
}

// A manual push stands auto-mode down for the rest of the session.
func TestAManualPushSuspendsIt(t *testing.T) {
	verdict := judge.Decide([]contract.Item{finding("a", contract.SeverityHigh)}, judge.Suspend(judge.State{}), on(), now)

	if len(verdict.Chosen) != 0 || !strings.Contains(verdict.Held, "suspended") {
		t.Errorf("pushed while suspended: %+v, held %q", verdict.Chosen, verdict.Held)
	}
}

func TestStampCountsThePush(t *testing.T) {
	got := judge.Stamp(judge.State{Pushes: 1}, now)

	if got.Pushes != 2 || !got.LastPush.Equal(now) {
		t.Errorf("Stamp = %+v", got)
	}
}

package contract_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
)

// Every item ever written is open until something says otherwise, so the zero
// value has to read as open rather than as an item in no state at all.
func TestZeroStateIsOpen(t *testing.T) {
	if got := (contract.Item{}).CurrentState(); got != contract.StateOpen {
		t.Errorf("CurrentState of a fresh item = %q, want %q", got, contract.StateOpen)
	}
}

func TestStands(t *testing.T) {
	cases := []struct {
		name string
		item contract.Item
		want bool
	}{
		{"fresh", contract.Item{}, true},
		{"confirmed", contract.Item{Verdict: contract.VerdictConfirmed}, true},
		{"dismissed", contract.Item{Verdict: contract.VerdictDismissed}, false},
		{"fixed", contract.Item{State: contract.StateFixed}, false},
		// Handing an item to an agent is not evidence that the agent dealt with
		// it. Only a later pass finding the code gone is.
		{"pushed", contract.Item{State: contract.StatePushed}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.item.Stands(); got != c.want {
				t.Errorf("Stands = %v, want %v", got, c.want)
			}
		})
	}
}

func TestPushable(t *testing.T) {
	cases := []struct {
		name string
		item contract.Item
		want bool
	}{
		{"open", contract.Item{}, true},
		{"accepted", contract.Item{State: contract.StateAccepted}, true},
		// The two rules that keep a push idempotent.
		{"dismissed", contract.Item{Verdict: contract.VerdictDismissed}, false},
		{"in flight", contract.Item{State: contract.StatePushed}, false},
		{"fixed", contract.Item{State: contract.StateFixed}, false},
		// Dismissal beats acceptance: somebody changed their mind.
		{"accepted then dismissed", contract.Item{State: contract.StateAccepted, Verdict: contract.VerdictDismissed}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.item.Pushable(); got != c.want {
				t.Errorf("Pushable = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSeverityAtLeast(t *testing.T) {
	cases := []struct {
		sev, floor contract.Severity
		want       bool
	}{
		{contract.SeverityHigh, contract.SeverityHigh, true},
		{contract.SeverityHigh, contract.SeverityLow, true},
		{contract.SeverityMedium, contract.SeverityHigh, false},
		{contract.SeverityLow, contract.SeverityMedium, false},
		// An analyser's own vocabulary normalises to medium, and the filter has
		// to agree with what the item will be shown as.
		{"blocker", contract.SeverityHigh, false},
		{"blocker", contract.SeverityMedium, true},
	}

	for _, c := range cases {
		if got := c.sev.AtLeast(c.floor); got != c.want {
			t.Errorf("Severity(%q).AtLeast(%q) = %v, want %v", c.sev, c.floor, got, c.want)
		}
	}
}

func TestSeverityNormalise(t *testing.T) {
	if got := contract.Severity("critical").Normalise(); got != contract.SeverityMedium {
		t.Errorf("an unknown level normalised to %q, want %q", got, contract.SeverityMedium)
	}
	for _, known := range contract.Severities {
		if got := known.Normalise(); got != known {
			t.Errorf("Normalise(%q) = %q, want it left alone", known, got)
		}
	}
}

func TestWhere(t *testing.T) {
	withLine := contract.Item{Location: contract.Location{Path: "internal/a.go", StartLine: 42}}
	if got, want := withLine.Where(), "internal/a.go:42"; got != want {
		t.Errorf("Where = %q, want %q", got, want)
	}

	fileWide := contract.Item{Location: contract.Location{Path: "internal/a.go"}}
	if got, want := fileWide.Where(), "internal/a.go"; got != want {
		t.Errorf("Where of a file-wide item = %q, want %q", got, want)
	}
}

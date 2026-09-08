package contract_test

import (
	"testing"

	"github.com/mondial7/mondspace-reviewer/contract"
)

func TestOnlyWhatIsStillToBeDealtWithIsActionable(t *testing.T) {
	// An agent's context is scarce. Handing it every note a human ever wrote
	// wastes it, and handing it approvals as though they were work is worse
	// (ADR 0031).
	tests := []struct {
		note contract.Item
		want bool
		why  string
	}{
		{contract.Item{Source: contract.SourceHuman, Kind: contract.KindObjection}, true, "an objection is a thing to change"},
		{contract.Item{Source: contract.SourceHuman, Kind: contract.KindQuestion}, true, "a question wants answering"},
		{contract.Item{Source: contract.SourceHuman, Kind: contract.KindDebt}, true, "debt is a thing to remember"},
		{contract.Item{Source: contract.SourceHuman, Kind: contract.KindOK}, false, "an approval is not work"},
		{contract.Item{Source: contract.SourceHuman, Kind: contract.KindNote}, false, "a remark is the reviewer thinking aloud"},
		{contract.Item{Source: contract.SourceHuman, Kind: contract.KindObjection, SupersededBy: "n2"}, false,
			"a superseded note has been dealt with"},
	}

	for _, tt := range tests {
		if got := tt.note.Actionable(); got != tt.want {
			t.Errorf("%+v Actionable = %v, want %v — %s", tt.note, got, tt.want, tt.why)
		}
	}
}

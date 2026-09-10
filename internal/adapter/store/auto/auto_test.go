package auto_test

import (
	"testing"
	"time"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/auto"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/judge"
)

// A manual push has to stand auto-mode down whatever session it is running
// under, and `msr push` does not know that id — it is the agent's, passed to
// `msr auto run`. Reading the state under the wrong id used to hand back a
// zero one, and writing that back both failed to suspend the session and reset
// its spent budget to nothing.
func TestSuspendingWithoutKnowingTheSession(t *testing.T) {
	dir := t.TempDir()
	store := auto.New(dir)
	if err := store.SaveState(judge.State{Session: "agent-7", Pushes: 4, LastPush: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := store.SetSuspended(true); err != nil {
		t.Fatal(err)
	}

	got, err := store.State("agent-7")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Suspended {
		t.Error("a manual push did not stand the running session down")
	}
	if got.Pushes != 4 {
		t.Errorf("pushes = %d, want the budget it had already spent (4)", got.Pushes)
	}
}

// And turning it back on lifts the suspension without spending or refunding
// anything.
func TestResumingKeepsTheBudget(t *testing.T) {
	dir := t.TempDir()
	store := auto.New(dir)
	if err := store.SaveState(judge.State{Session: "agent-7", Pushes: 4, Suspended: true}); err != nil {
		t.Fatal(err)
	}

	if err := store.SetSuspended(false); err != nil {
		t.Fatal(err)
	}

	got, _ := store.State("agent-7")
	if got.Suspended || got.Pushes != 4 {
		t.Errorf("state = %+v, want it resumed with its budget intact", got)
	}
}

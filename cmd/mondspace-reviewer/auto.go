package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/mondial7/mondspace-reviewer/contract"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/delivery"
	gitsnap "github.com/mondial7/mondspace-reviewer/internal/adapter/snapshot/git"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/auto"
	"github.com/mondial7/mondspace-reviewer/internal/adapter/store/items"
	"github.com/mondial7/mondspace-reviewer/internal/usecase"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/handoff"
	"github.com/mondial7/mondspace-reviewer/internal/usecase/judge"
)

// Auto-mode, from the outside (ADR 0047).
//
//	msr auto on | off | status
//	msr auto run --session=<id>   consider a push, at a checkpoint
//
// `run` is what a hook calls when the agent reaches a checkpoint. It is a
// separate command rather than a daemon because the thing that knows an agent
// is between tasks is the agent, and asking msr to guess would mean pushing
// mid-edit.
func runAuto(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("auto", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository")
	dir := fs.String("dir", "", "shared directory holding the findings store (default <repo>/.mondspace)")
	branch := fs.String("branch", "", "which branch's findings (default: the one checked out)")
	session := fs.String("session", "", "which agent session this is, for the per-session budget")
	to := fs.String("to", "file", "where a judged push is sent (file|stdout|clipboard)")
	dryRun := fs.Bool("dry-run", false, "decide and log, but send nothing")
	flags, words := partition(fs, args[1:])
	if err := fs.Parse(flags); err != nil {
		return err
	}
	verb := "status"
	if len(words) > 0 {
		verb = words[0]
	}

	shared := sharedDir(*repo, *dir)
	state := auto.New(shared)
	policy, err := state.Policy()
	if err != nil {
		return fmt.Errorf("reading the auto-mode policy: %w", err)
	}

	switch verb {
	case "on":
		policy.Enabled = true
		if err := state.SavePolicy(policy); err != nil {
			return err
		}
		// Turning it on clears a suspension: saying so is the explicit act that
		// a manual push stood down. Through the store rather than through a
		// read-modify-write of one session's state, which would reset whatever
		// budget the running session had already spent.
		if err := state.SetSuspended(false); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "auto-mode on: at most %d push(es) a session, %d item(s) each, %s apart, %s and above\n",
			policy.MaxPushes, policy.MaxPerBatch, policy.Cooldown, policy.MinSeverity)
		return nil
	case "off":
		policy.Enabled = false
		if err := state.SavePolicy(policy); err != nil {
			return err
		}
		// Read before the next decision rather than held in a process, so this
		// takes effect at the next checkpoint with nothing in flight escaping.
		fmt.Fprintln(stdout, "auto-mode off")
		return nil
	case "status":
		return autoStatus(stdout, state, policy, *session)
	case "run":
		return autoRun(ctx, stdout, state, policy, *repo, shared, *branch, *session, *to, *dryRun)
	default:
		return fmt.Errorf("unknown auto command %q — try on, off, status or run", verb)
	}
}

func autoStatus(stdout io.Writer, store *auto.Store, policy judge.Policy, session string) error {
	current, err := store.State(session)
	if err != nil {
		return err
	}
	status := "off"
	if policy.Enabled {
		status = "on"
	}
	fmt.Fprintf(stdout, "auto-mode %s\n", status)
	fmt.Fprintf(stdout, "  policy: %d push(es) a session, %d item(s) each, %s cooldown, %s and above\n",
		policy.MaxPushes, policy.MaxPerBatch, policy.Cooldown, policy.MinSeverity)
	fmt.Fprintf(stdout, "  this session: %d push(es)", current.Pushes)
	if current.Suspended {
		fmt.Fprint(stdout, ", suspended by a manual push")
	}
	fmt.Fprintln(stdout)

	decisions, err := store.Decisions()
	if err != nil {
		return err
	}
	chosen := 0
	for _, decision := range decisions {
		if decision.Chosen {
			chosen++
		}
	}
	fmt.Fprintf(stdout, "  decisions logged: %d (%d chosen, %d held back) in %s\n",
		len(decisions), chosen, len(decisions)-chosen, auto.LogFile)
	return nil
}

// autoRun is one judged push, at a checkpoint.
func autoRun(ctx context.Context, stdout io.Writer, store *auto.Store, policy judge.Policy,
	repo, shared, branch, session, to string, dryRun bool) error {

	findings := items.New(shared)
	on := branchName(ctx, gitsnap.New(repo, "auto"), branch)
	stored, err := findings.OnBranch(on)
	if err != nil {
		return err
	}
	state, err := store.State(session)
	if err != nil {
		return err
	}

	at := time.Now().UTC()
	verdict := judge.Decide(standing(stored), state, policy, at)

	// Logged before anything is sent, so that a push which fails halfway still
	// leaves the reasoning behind it on disk.
	if err := store.Log(verdict.Decisions...); err != nil {
		return err
	}
	if len(verdict.Chosen) == 0 {
		fmt.Fprintf(stdout, "nothing pushed: %s\n", verdict.Held)
		return nil
	}

	brief := handoff.Assemble("judge-"+newULID(), on, at, verdict.Chosen)
	if dryRun {
		fmt.Fprintf(stdout, "would push %d item(s)\n", len(brief.Items))
		_, err := io.WriteString(stdout, handoff.Render(brief))
		return err
	}

	adapter, err := delivery.Pick(to, shared, stdout)
	if err != nil {
		return err
	}
	if err := adapter.Send(brief); err != nil {
		return fmt.Errorf("delivering the brief: %w", err)
	}
	if err := findings.Append(handoff.MarkPushed(brief, "judge")...); err != nil {
		return err
	}
	if err := store.SaveState(judge.Stamp(state, at)); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "the judge pushed %d item(s) as %s\n", len(brief.Items), brief.Batch)
	return nil
}

// standing is what the judge is allowed to see: what is still outstanding, and
// what this change caused.
//
// The second half matters more for the judge than for anybody else. A human
// reading a list can tell "this is not mine" at a glance; an agent handed a
// directive cannot, and would go and change code the work it is doing never
// touched (ADR 0053).
func standing(stored []contract.Item) []contract.Item {
	caused, _ := usecase.Caused(stored)

	var out []contract.Item
	for _, item := range caused {
		if item.Stands() {
			out = append(out, item)
		}
	}
	return out
}

// suspendAuto stands auto-mode down for the rest of the session, which is what
// a manual push does (ADR 0047).
//
// It does not name a session, because `msr push` does not know one: the id
// belongs to whoever calls `msr auto run`. Asking the store to set the flag on
// whatever is there is the whole of it.
//
// Best effort: a manual push must not fail because auto-mode's state file could
// not be written, and auto-mode being on is already the opt-in.
func suspendAuto(shared string) {
	_ = auto.New(shared).SetSuspended(true)
}

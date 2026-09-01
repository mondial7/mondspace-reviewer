package routed_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mondial7/mondspace-reviewer/internal/adapter/summarizer/routed"
	"github.com/mondial7/mondspace-reviewer/internal/domain"
	"github.com/mondial7/mondspace-reviewer/internal/port"
)

type stub struct {
	reply string
	err   error
	calls int
}

func (s *stub) Answer(context.Context, string, domain.AskContext) (string, error) {
	s.calls++
	return s.reply, s.err
}

func (s *stub) Headline(context.Context, domain.Unit, domain.Diff) (domain.Headline, error) {
	return domain.Headline{}, s.err
}

func TestThePrimaryAnswersAndSaysSo(t *testing.T) {
	primary := &stub{reply: "from the cli"}
	standby := &stub{reply: "from the local model"}
	s := routed.New(primary, domain.EngineCLI, standby, domain.EngineLocal)

	got, err := s.Answer(context.Background(), "q", domain.AskContext{})
	if err != nil || got != "from the cli" {
		t.Fatalf("Answer = %q, %v", got, err)
	}
	if standby.calls != 0 {
		t.Error("the standby was called for no reason")
	}
	engine, fellBack, _ := s.Answered()
	if engine != domain.EngineCLI || fellBack {
		t.Errorf("Answered = %q, fallback=%v; want the cli, no fallback", engine, fellBack)
	}
}

func TestAMissingEngineIsAnsweredByTheOneBehindIt(t *testing.T) {
	// The review must never block on an engine. It must also never pretend the
	// answer came from the engine it was routed to.
	primary := &stub{err: errors.New("claude: command not found")}
	standby := &stub{reply: "from the local model"}
	s := routed.New(primary, domain.EngineCLI, standby, domain.EngineLocal)

	got, err := s.Answer(context.Background(), "q", domain.AskContext{})
	if err != nil || got != "from the local model" {
		t.Fatalf("Answer = %q, %v", got, err)
	}
	engine, fellBack, why := s.Answered()
	if engine != domain.EngineLocal || !fellBack {
		t.Errorf("Answered = %q, fallback=%v; want the local model, marked as fallback", engine, fellBack)
	}
	if !strings.Contains(why, "command not found") {
		t.Errorf("why = %q, want the primary's failure", why)
	}
}

func TestBothEnginesGoneReportsTheOneItWasRoutedTo(t *testing.T) {
	primary := &stub{err: errors.New("no claude")}
	standby := &stub{err: errors.New("no server")}
	s := routed.New(primary, domain.EngineCLI, standby, domain.EngineLocal)

	if _, err := s.Answer(context.Background(), "q", domain.AskContext{}); err == nil {
		t.Fatal("two dead engines should be an error")
	} else if !strings.Contains(err.Error(), "no claude") {
		t.Errorf("err = %v, want the routed engine's failure named first", err)
	}
	if engine, fellBack, _ := s.Answered(); engine != "" || fellBack {
		t.Errorf("nothing answered, so nothing should be claimed: %q %v", engine, fellBack)
	}
}

func TestACancelledCallIsNotRetriedOnTheOtherEngine(t *testing.T) {
	// A second call nobody is waiting for, and on a paid engine a second bill.
	primary := &stub{err: context.Canceled}
	standby := &stub{reply: "should not run"}
	s := routed.New(primary, domain.EngineCLI, standby, domain.EngineLocal)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Answer(ctx, "q", domain.AskContext{}); err == nil {
		t.Fatal("a cancelled call should fail")
	}
	if standby.calls != 0 {
		t.Error("a cancelled call was retried on the standby")
	}
}

func TestHeadlinesGoStraightToTheStandby(t *testing.T) {
	// Not a failure path: the CLI declines headlines on purpose, because a
	// hundred per review through a paid session is a bill nobody asked for.
	primary := &stub{err: errors.New("the claude cli answers whole questions")}
	standby := &stub{}
	s := routed.New(primary, domain.EngineCLI, standby, domain.EngineLocal)

	if _, err := s.Headline(context.Background(), domain.Unit{}, domain.Diff{}); err != nil {
		t.Fatalf("Headline: %v", err)
	}
}

func TestAnEngineWithNothingBehindItDoesNotInventOne(t *testing.T) {
	primary := &stub{err: errors.New("gone")}
	s := routed.New(primary, domain.EngineLocal, nil, "")

	if _, err := s.Answer(context.Background(), "q", domain.AskContext{}); err == nil {
		t.Fatal("want the failure through")
	}
	if _, fellBack, _ := s.Answered(); fellBack {
		t.Error("there was nothing to fall back to")
	}
}

var _ port.Summarizer = (*routed.Summarizer)(nil)
var _ port.EngineReporter = (*routed.Summarizer)(nil)

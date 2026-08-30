package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAQuietRunWritesNothingAtAll(t *testing.T) {
	// Redirected to a file or read by a script, a spinner is a few hundred
	// carriage returns in a log. Not fewer frames — none.
	var b bytes.Buffer
	p := newProgress(&b, false)

	p.step("reading the change")(nil)
	p.count("described", 3, 7)

	if b.Len() != 0 {
		t.Errorf("wrote %q with nothing watching", b.String())
	}
}

func TestAFinishedStepSaysWhatItDidAndHowLongItTook(t *testing.T) {
	// The reason to show anything: a local model call is seconds to minutes,
	// and a terminal that prints nothing for a minute looks broken.
	var b bytes.Buffer
	p := newProgress(&b, true)

	done := p.step("asking the model")
	time.Sleep(15 * time.Millisecond)
	done(nil)

	got := b.String()
	if !strings.Contains(got, "asking the model") {
		t.Errorf("the step should be named:\n%q", got)
	}
	if !strings.Contains(got, "ms") && !strings.Contains(got, "s") {
		t.Errorf("the step should say how long it took:\n%q", got)
	}
}

func TestAFailedStepSaysSo(t *testing.T) {
	var b bytes.Buffer
	p := newProgress(&b, true)

	p.step("asking the model")(errors.New("connection refused"))

	got := b.String()
	if !strings.Contains(got, "connection refused") {
		t.Errorf("a failure should carry its reason:\n%q", got)
	}
}

func TestACountedStepSaysHowFarAlongItIs(t *testing.T) {
	// "3/7 described" is the difference between waiting and wondering.
	var b bytes.Buffer
	p := newProgress(&b, true)

	p.count("described", 3, 7)

	got := b.String()
	for _, want := range []string{"3", "7", "described"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%q", want, got)
		}
	}
}

func TestEachStepClearsTheLineItLeaves(t *testing.T) {
	// The spinner overwrites one line. Without the erase, a short label after a
	// long one leaves the tail of the long one on screen forever.
	var b bytes.Buffer
	p := newProgress(&b, true)

	p.count("a much longer label than the next", 1, 2)
	p.count("short", 2, 2)

	if !strings.Contains(b.String(), "\x1b[2K") {
		t.Errorf("want the line erased between updates:\n%q", b.String())
	}
}

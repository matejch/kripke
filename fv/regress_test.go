package fv

import (
	"strings"
	"testing"
)

func TestTooManyInitialStatesIsAnError(t *testing.T) {
	m := counter(1)
	m.Init = func() []State {
		out := make([]State, 40000)
		for i := range out {
			out[i] = &cnt{N: i}
		}
		return out
	}
	if r := Run(m, Options{}); !strings.Contains(r.Error, "limit is 32767") {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestFailedExpansionMarksTheRunIncomplete(t *testing.T) {
	m := counter(5)
	m.Actions = append(m.Actions, act("boom", func(c *Choice, s *cnt) bool {
		if s.N == 1 {
			panic("kaboom")
		}
		return false
	}))
	r := Run(m, Options{ContinueAfterViolation: true})
	if r.Complete || r.CyclesChecked || !strings.Contains(r.Incomplete, "expansion failed") {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestEncodeErrorDoesNotPoisonThePlanCache(t *testing.T) {
	m := Model{Name: "chan", Init: func() []State { return []State{&withChan{}} }, Actions: []Action{{Name: "x", Step: func(*Choice, State) State { return nil }}}}
	for range 2 {
		if r := Run(m, Options{}); !strings.Contains(r.Error, "cannot encode chan") {
			t.Fatalf("got %q", r.Error)
		}
	}
}

func TestSimulationReportsEachViolationOnce(t *testing.T) {
	m := counter(1000)
	m.Invariants = []Invariant{Inv("n < 10", func(s State) bool { return n(s) < 10 })}
	r := Run(m, Options{Simulate: 50, MaxDepth: 50, ContinueAfterViolation: true})
	if len(r.Violations) != 1 {
		t.Fatalf("want 1 violation, got %d", len(r.Violations))
	}
}

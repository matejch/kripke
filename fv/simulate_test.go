package fv

import "testing"

func TestSimulationFindsAViolation(t *testing.T) {
	m := counter(1000)
	m.Invariants = []Invariant{Inv("n < 50", func(s State) bool { return n(s) < 50 })}
	r := Run(m, Options{Simulate: 20, MaxDepth: 100, Seed: 7})
	v := firstViolation(t, r, KindInvariant)
	if r.Complete || n2(v) < 50 {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func n2(v Violation) int {
	last := v.Trace.Steps[len(v.Trace.Steps)-1].State
	for _, f := range last {
		if f.Path == "N" {
			var x int
			for _, ch := range f.Value {
				x = x*10 + int(ch-'0')
			}
			return x
		}
	}
	return -1
}

func TestSimulationIsIndependentOfWorkers(t *testing.T) {
	m := counter(1000)
	m.Invariants = []Invariant{Inv("n < 40", func(s State) bool { return n(s) < 40 })}
	a := Run(m, Options{Simulate: 30, MaxDepth: 60, Seed: 3, Workers: 1})
	b := Run(m, Options{Simulate: 30, MaxDepth: 60, Seed: 3, Workers: 8})
	if a.States != b.States || Render(a.Violations[0]) != Render(b.Violations[0]) {
		t.Fatalf("differ:\n%s\n%s", RenderResult(a), RenderResult(b))
	}
}

func TestSimulationStopsAtQuiescence(t *testing.T) {
	m := counter(5)
	m.DeadlockIsError = true
	firstViolation(t, Run(m, Options{Simulate: 5}), KindDeadlock)
}

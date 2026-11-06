package fv

import (
	"strings"
	"testing"
)

func TestShortestTrace(t *testing.T) {
	m := counter(5)
	m.Invariants = []Invariant{Inv("n < 4", func(s State) bool { return n(s) < 4 })}
	r := Run(m, Options{})
	if len(r.Violations) != 1 || r.Violations[0].Steps() != 2 {
		t.Fatalf("want a 2-step trace, got %s", RenderResult(r))
	}
	if got := r.Violations[0].Trace.Steps[2].Choices; len(got) != 1 {
		t.Fatalf("choices not recorded: %v", got)
	}
}

func TestInvariantOnInitialState(t *testing.T) {
	m := counter(3)
	m.Invariants = []Invariant{Inv("never", func(State) bool { return false })}
	r := Run(m, Options{})
	if len(r.Violations) != 1 || r.Violations[0].Steps() != 0 {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestMaxDepthMarksIncomplete(t *testing.T) {
	r := Run(counter(20), Options{MaxDepth: 3})
	if r.Complete || r.CyclesChecked || r.Depth != 3 || !strings.Contains(r.Incomplete, "max depth") {
		t.Fatalf("got %s", RenderResult(r))
	}
	if r := Run(counter(5), Options{MaxDepth: 5}); !r.Complete {
		t.Fatalf("bound equal to the diameter should be complete: %s", RenderResult(r))
	}
}

func TestMaxStates(t *testing.T) {
	r := Run(counter(100), Options{MaxStates: 10})
	if r.Complete || r.States != 10 {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestDFSFindsTheSameStates(t *testing.T) {
	a, b := Run(counter(30), Options{}), Run(counter(30), Options{DFS: true})
	if a.States != b.States || a.Transitions != b.Transitions {
		t.Fatalf("bfs %d/%d dfs %d/%d", a.States, a.Transitions, b.States, b.Transitions)
	}
}

func TestDeadlock(t *testing.T) {
	m := counter(4)
	m.DeadlockIsError = true
	v := firstViolation(t, Run(m, Options{}), KindDeadlock)
	if v.Steps() != 2 {
		t.Fatalf("want deadlock at N=4 after 2 steps\n%s", v)
	}
}

type mapState struct {
	Seen map[int]bool
	Out  []int
}

func (s *mapState) Clone() State {
	c := &mapState{Seen: map[int]bool{}, Out: append([]int(nil), s.Out...)}
	for k, v := range s.Seen {
		c.Seen[k] = v
	}
	return c
}

func TestDeterminismCheckCatchesMapIteration(t *testing.T) {
	m := Model{Name: "maporder", Init: func() []State {
		return []State{&mapState{Seen: map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true, 8: true}}}
	}, Actions: []Action{{Name: "drain", Step: func(c *Choice, s State) State {
		st := s.(*mapState)
		for k := range st.Seen {
			st.Out = append(st.Out, k)
			delete(st.Seen, k)
			return st
		}
		return nil
	}}}}
	firstViolation(t, Run(m, Options{}), KindDeterminism)
}

type shallow struct{ Xs []int }

func (s *shallow) Clone() State { c := *s; return &c }

func TestDeterminismCheckCatchesShallowClone(t *testing.T) {
	m := Model{Name: "shallow", Init: func() []State { return []State{&shallow{Xs: []int{0}}} }, Actions: []Action{{Name: "bump", Step: func(c *Choice, s State) State {
		st := s.(*shallow)
		if st.Xs[0] >= 3 {
			return nil
		}
		st.Xs[0]++
		return st
	}}}}
	v := firstViolation(t, Run(m, Options{}), KindDeterminism)
	if !strings.Contains(v.Detail, "Clone") {
		t.Fatalf("detail should blame Clone: %s", v.Detail)
	}
}

func TestPanicBecomesAViolation(t *testing.T) {
	m := counter(5)
	m.Actions = append(m.Actions, act("boom", func(c *Choice, s *cnt) bool {
		if s.N == 2 {
			panic("kaboom")
		}
		return false
	}))
	v := firstViolation(t, Run(m, Options{}), KindPanic)
	if v.Name != "action boom" || !strings.Contains(v.Detail, "kaboom") {
		t.Fatalf("got %s", v)
	}
}

func TestContinueReportsEveryProperty(t *testing.T) {
	m := counter(6)
	m.Invariants = []Invariant{
		Inv("lt 3", func(s State) bool { return n(s) < 3 }),
		Inv("lt 5", func(s State) bool { return n(s) < 5 }),
	}
	r := Run(m, Options{ContinueAfterViolation: true})
	if len(r.Violations) != 2 || !r.Complete {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestLimitsAreEnforced(t *testing.T) {
	m := counter(1)
	for i := range 64 {
		m.Actions = append(m.Actions, act(string(rune('a'+i%26))+string(rune('a'+i/26)), func(*Choice, *cnt) bool { return false }))
	}
	if r := Run(m, Options{}); !strings.Contains(r.Error, "limit is 64") {
		t.Fatalf("want an action limit error, got %q", r.Error)
	}
}

func TestWorkersGiveIdenticalResults(t *testing.T) {
	m := counter(200)
	m.Invariants = []Invariant{Inv("lt 150", func(s State) bool { return n(s) < 150 })}
	a, b := Run(m, Options{Workers: 1}), Run(m, Options{Workers: 7})
	if a.States != b.States || a.Transitions != b.Transitions || Render(a.Violations[0]) != Render(b.Violations[0]) {
		t.Fatalf("differ:\n%s\n%s", RenderResult(a), RenderResult(b))
	}
}

func TestJSONRoundTrip(t *testing.T) {
	m := counter(5)
	m.Invariants = []Invariant{Inv("n < 4", func(s State) bool { return n(s) < 4 })}
	r := Run(m, Options{})
	var b strings.Builder
	if err := WriteJSON(&b, r); err != nil {
		t.Fatal(err)
	}
	back, err := ReadJSON(strings.NewReader(b.String()))
	if err != nil || len(back) != 1 || RenderResult(back[0]) != RenderResult(r) {
		t.Fatalf("round trip changed the result: %v\n%s", err, b.String())
	}
}

func firstViolation(t *testing.T, r Result, kind string) Violation {
	t.Helper()
	for _, v := range r.Violations {
		if v.Kind == kind {
			return v
		}
	}
	t.Fatalf("want a %s violation, got %s", kind, RenderResult(r))
	return Violation{}
}

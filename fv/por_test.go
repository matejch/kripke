package fv

import (
	"strings"
	"testing"
)

type two struct{ A, B int }

func (s *two) Clone() State { c := *s; return &c }

func twoCounters(declared bool, invReads []string) Model {
	inc := func(name string, f func(*two) *int, field string) Action {
		a := Action{Name: name, Step: func(c *Choice, s State) State {
			p := f(s.(*two))
			if *p >= 6 {
				return nil
			}
			*p++
			return s
		}}
		if declared {
			a.Reads, a.Writes = []string{field}, []string{field}
		}
		return a
	}
	return Model{
		Name:       "two",
		Init:       func() []State { return []State{&two{}} },
		Actions:    []Action{inc("a", func(t *two) *int { return &t.A }, "A"), inc("b", func(t *two) *int { return &t.B }, "B")},
		Invariants: []Invariant{{Name: "a small", Holds: func(s State) bool { return s.(*two).A < 6 }, Reads: invReads}},
	}
}

func TestPORReducesIndependentCounters(t *testing.T) {
	full := Run(twoCounters(true, []string{"A"}), Options{ContinueAfterViolation: true})
	red := Run(twoCounters(true, []string{"A"}), Options{POR: true, ContinueAfterViolation: true})
	if red.States >= full.States || len(red.Violations) != 1 || len(full.Violations) != 1 {
		t.Fatalf("full %s\nreduced %s", RenderResult(full), RenderResult(red))
	}
}

func TestPORWithoutDeclarationsChangesNothing(t *testing.T) {
	a := Run(twoCounters(false, nil), Options{ContinueAfterViolation: true})
	b := Run(twoCounters(false, nil), Options{POR: true, ContinueAfterViolation: true})
	if a.States != b.States {
		t.Fatalf("undeclared actions are dependent on everything: %d vs %d", a.States, b.States)
	}
}

func TestPORRefusesLiveness(t *testing.T) {
	m := twoCounters(true, nil)
	m.Eventually = []Invariant{Inv("x", func(State) bool { return true })}
	if r := Run(m, Options{POR: true}); !strings.Contains(r.Error, "safety only") {
		t.Fatalf("got %q", r.Error)
	}
}

func TestPORValidateCatchesAWrongDeclaration(t *testing.T) {
	m := twoCounters(true, nil)
	m.Actions[1].Step = func(c *Choice, s State) State {
		st := s.(*two)
		if st.B >= 6 {
			return nil
		}
		st.B++
		st.A = 0
		return s
	}
	firstViolation(t, Run(m, Options{PORValidate: true}), KindPOR)
}

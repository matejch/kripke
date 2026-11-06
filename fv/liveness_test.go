package fv

import "testing"

func edges(m *Model, list ...any) {
	for i := 0; i < len(list); i += 4 {
		name, from, to, fair := list[i].(string), list[i+1].(int), list[i+2].(int), list[i+3].(string)
		a := act(name, func(c *Choice, s *cnt) bool {
			if s.N != from {
				return false
			}
			s.N = to
			return true
		})
		a.Fair, a.StrongFair = fair == "weak", fair == "strong"
		m.Actions = append(m.Actions, a)
	}
}

func lgraph(p func(int) bool) Model {
	return Model{Name: "g", Init: func() []State { return []State{&cnt{}} }, Eventually: []Invariant{Inv("P", func(s State) bool { return p(n(s)) })}}
}

func TestStrongFairnessFindsTheFairSubcycle(t *testing.T) {
	m := lgraph(func(n int) bool { return n == 3 })
	edges(&m, "ab", 0, 1, "", "ba", 1, 0, "", "bc", 1, 2, "", "ca", 2, 0, "", "s", 2, 3, "strong")
	v := firstViolation(t, Run(m, Options{}), KindLiveness)
	for _, st := range v.Trace.Steps {
		if st.Action == "bc" {
			t.Fatalf("the fair loop must avoid state 2, where s is enabled\n%s", v)
		}
	}
}

func TestStrongFairnessExcludesTheStarvingCycle(t *testing.T) {
	m := lgraph(func(n int) bool { return n == 3 })
	edges(&m, "ab", 0, 1, "", "ba", 1, 2, "", "ca", 2, 0, "", "s", 2, 3, "strong")
	if r := Run(m, Options{}); !r.OK() || !r.CyclesChecked {
		t.Fatalf("every cycle passes state 2, so strong fairness forces s:\n%s", RenderResult(r))
	}
}

func TestWeakFairnessNeedsContinuousEnabling(t *testing.T) {
	m := lgraph(func(n int) bool { return n == 3 })
	edges(&m, "ab", 0, 1, "", "ba", 1, 2, "", "ca", 2, 0, "", "s", 2, 3, "weak")
	firstViolation(t, Run(m, Options{}), KindLiveness)
	m = lgraph(func(n int) bool { return n == 3 })
	edges(&m, "loop", 0, 0, "", "s", 0, 3, "weak")
	if r := Run(m, Options{}); !r.OK() {
		t.Fatalf("s is always enabled on the self-loop, so the loop is unfair:\n%s", RenderResult(r))
	}
}

func TestUnfairCycleViolatesEventually(t *testing.T) {
	m := lgraph(func(n int) bool { return n == 3 })
	edges(&m, "loop", 0, 0, "", "s", 0, 3, "")
	v := firstViolation(t, Run(m, Options{}), KindLiveness)
	if v.Trace.LoopsTo != 0 || v.Steps() != 1 {
		t.Fatalf("want a one-step self loop\n%s", v)
	}
}

func TestEventuallyAtQuiescence(t *testing.T) {
	m := lgraph(func(n int) bool { return n == 2 })
	edges(&m, "a", 0, 1, "", "b", 0, 2, "")
	v := firstViolation(t, Run(m, Options{}), KindEventually)
	if v.Steps() != 1 {
		t.Fatalf("got\n%s", v)
	}
}

func TestIncompleteRunSkipsCycles(t *testing.T) {
	m := lgraph(func(n int) bool { return false })
	edges(&m, "a", 0, 1, "", "b", 1, 2, "", "c", 2, 0, "")
	r := Run(m, Options{MaxDepth: 1})
	if r.CyclesChecked || len(r.Violations) != 0 {
		t.Fatalf("got %s", RenderResult(r))
	}
}

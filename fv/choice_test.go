package fv

import "testing"

func TestPickEnumeratesEveryBranch(t *testing.T) {
	r := Run(counter(5), Options{})
	if r.States != 7 || !r.Complete {
		t.Fatalf("want 7 states (0..6), got %s", RenderResult(r))
	}
}

func TestNestedPicksFormATree(t *testing.T) {
	m := Model{Name: "tree", Init: func() []State { return []State{&cnt{}} }, Actions: []Action{act("set", func(c *Choice, s *cnt) bool {
		if s.N != 0 {
			return false
		}
		a := c.Pick(3)
		b := 0
		if a == 2 {
			b = c.Pick(4)
		}
		s.N, s.M = 1+a, b
		return true
	})}}
	r := Run(m, Options{})
	if r.States != 1+2+4 || r.Transitions != 6 {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestAnyAndBool(t *testing.T) {
	m := Model{Name: "any", Init: func() []State { return []State{&cnt{}} }, Actions: []Action{act("set", func(c *Choice, s *cnt) bool {
		if s.N != 0 {
			return false
		}
		s.N = Any(c, []int{10, 20, 30})
		if c.Bool() {
			s.M = 1
		}
		return true
	})}}
	if r := Run(m, Options{}); r.States != 7 {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestPickOfOneIsNotABranch(t *testing.T) {
	var c Choice
	c.reset(nil, false)
	if c.Pick(1) != 0 || len(c.Choices()) != 0 || c.next() != nil {
		t.Fatal("Pick(1) should not record a choice")
	}
}

package fv

import "testing"

func rgraph() Model {
	return Model{Name: "r", Init: func() []State { return []State{&cnt{}} }, Responses: []Response{Resp("1 leads to 3",
		func(s State) bool { return n(s) == 1 }, func(s State) bool { return n(s) == 3 })}}
}

func TestResponseHoldsUnderFairness(t *testing.T) {
	m := rgraph()
	edges(&m, "req", 0, 1, "", "wait", 1, 2, "", "spin", 2, 1, "", "serve", 2, 3, "strong", "reset", 3, 0, "")
	if r := Run(m, Options{}); !r.OK() || !r.CyclesChecked {
		t.Fatalf("got %s", RenderResult(r))
	}
}

func TestResponseFailsOnAFairCycle(t *testing.T) {
	m := rgraph()
	edges(&m, "req", 0, 1, "", "wait", 1, 2, "", "spin", 2, 1, "", "serve", 2, 3, "weak", "reset", 3, 0, "")
	v := firstViolation(t, Run(m, Options{}), KindResponse)
	if v.Trace.LoopsTo < 0 {
		t.Fatalf("want a lasso\n%s", v)
	}
}

func TestResponseFailsAtQuiescence(t *testing.T) {
	m := rgraph()
	edges(&m, "req", 0, 1, "", "give-up", 1, 4, "", "serve", 1, 3, "")
	v := firstViolation(t, Run(m, Options{}), KindResponse)
	if v.Trace.LoopsTo != -1 || v.Steps() != 2 {
		t.Fatalf("want req then give-up\n%s", v)
	}
}

func TestResponseIgnoresUntriggeredCycles(t *testing.T) {
	m := rgraph()
	edges(&m, "idle", 0, 0, "", "req", 0, 1, "", "serve", 1, 3, "weak")
	if r := Run(m, Options{}); !r.OK() {
		t.Fatalf("got %s", RenderResult(r))
	}
}

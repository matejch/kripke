package fv

import (
	"fmt"
	"testing"
)

// Check runs the model and fails t with the rendered result on any violation or error.
func Check(t testing.TB, m Model, o Options) Result {
	t.Helper()
	r := Run(m, o)
	if !r.OK() {
		t.Fatalf("\n%s", RenderResult(r))
	}
	return r
}

// Expect fails t unless the run reports a violation of the given kind and name, and returns it.
func Expect(t testing.TB, m Model, o Options, kind, name string) Violation {
	t.Helper()
	r := Run(m, o)
	if r.Error != "" {
		t.Fatalf("\n%s", RenderResult(r))
	}
	for _, v := range r.Violations {
		if v.Kind == kind && v.Name == name {
			return v
		}
	}
	t.Fatalf("expected %s %q in %s, got:\n%s", kind, name, m.Name, RenderResult(r))
	return Violation{}
}

// Steps is the number of transitions in a violation's trace.
func (v Violation) Steps() int { return max(len(v.Trace.Steps)-1, 0) }

// String renders the violation.
func (v Violation) String() string { return fmt.Sprint(Render(v)) }

package twophase

import (
	"testing"

	"github.com/matej/kripke/fv"
)

func TestFixedIsCleanAndComplete(t *testing.T) {
	r := fv.Check(t, Fixed, fv.Options{})
	if !r.Complete || !r.CyclesChecked {
		t.Fatalf("incomplete: %+v", r)
	}
	t.Logf("%d states", r.States)
}

func TestUnilateralAbortBreaksConsistency(t *testing.T) {
	v := fv.Expect(t, Unilateral, fv.Options{}, fv.KindInvariant, "consistent")
	if v.Steps() != 9 {
		t.Fatalf("want 9 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestSymmetryShrinksStateSpace(t *testing.T) {
	sym := fv.Check(t, Fixed, fv.Options{})
	full := fv.Check(t, Fixed, fv.Options{NoSymmetry: true})
	if full.States <= 2*sym.States {
		t.Fatalf("symmetry gave %d vs %d", sym.States, full.States)
	}
	t.Logf("symmetry %d, without %d", sym.States, full.States)
}

func TestWorkerCountDoesNotChangeResult(t *testing.T) {
	a := fv.Run(Unilateral, fv.Options{Workers: 1})
	b := fv.Run(Unilateral, fv.Options{Workers: 8})
	if a.States != b.States || a.Transitions != b.Transitions || fv.Render(a.Violations[0]) != fv.Render(b.Violations[0]) {
		t.Fatalf("workers changed the result:\n%s\n%s", fv.RenderResult(a), fv.RenderResult(b))
	}
}

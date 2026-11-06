package transfer

import (
	"testing"

	"github.com/matejch/kripke/fv"
)

func TestAtomicConservesMoney(t *testing.T) {
	r := fv.Check(t, Atomic, fv.Options{})
	if !r.Complete || !r.CyclesChecked {
		t.Fatalf("incomplete: %s", fv.RenderResult(r))
	}
}

func TestNonAtomicLosesAnUpdate(t *testing.T) {
	v := fv.Expect(t, NonAtomic, fv.Options{}, fv.KindInvariant, "money conserved")
	if v.Steps() != 4 {
		t.Fatalf("want 4 steps, got %d\n%s", v.Steps(), v)
	}
	t.Logf("\n%s", v)
}

func TestSymmetryShrinksStateSpace(t *testing.T) {
	o := fv.Options{ContinueAfterViolation: true}
	sym := fv.Run(NonAtomic, o)
	o.NoSymmetry = true
	full := fv.Run(NonAtomic, o)
	if !sym.Complete || !full.Complete || full.States < 2*sym.States {
		t.Fatalf("symmetry %d vs %d", sym.States, full.States)
	}
	t.Logf("symmetry %d, without %d", sym.States, full.States)
}

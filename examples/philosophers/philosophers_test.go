package philosophers

import (
	"testing"

	"github.com/matej/kripke/fv"
)

func clean(t *testing.T, m fv.Model) {
	t.Helper()
	r := fv.Check(t, m, fv.Options{})
	if !r.Complete || !r.CyclesChecked {
		t.Fatalf("incomplete: %s", fv.RenderResult(r))
	}
	t.Logf("%s", fv.RenderResult(r))
}

func TestPoliteEveryoneEats(t *testing.T) { clean(t, Polite) }

func TestNaiveDeadlocksBeforeEveryoneEats(t *testing.T) {
	v := fv.Expect(t, Naive, fv.Options{}, fv.KindEventually, "everyone ate")
	if v.Steps() != 6 {
		t.Fatalf("want 6 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestForeverPoliteNeverDeadlocksOrStarves(t *testing.T) { clean(t, Forever) }

func TestForeverNaiveDeadlocks(t *testing.T) {
	v := fv.Expect(t, ForeverNaive, fv.Options{}, fv.KindDeadlock, "deadlock")
	if v.Steps() != 6 {
		t.Fatalf("want 6 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestWeakFairnessLetsANeighbourStarvePhilosopherZero(t *testing.T) {
	v := fv.Expect(t, ForeverWeak, fv.Options{}, fv.KindResponse, "hungry 0 eventually eats")
	if v.Trace.LoopsTo < 0 {
		t.Fatalf("want a lasso\n%s", v)
	}
	t.Logf("\n%s", v)
}

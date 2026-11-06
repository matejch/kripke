package peterson

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

func TestBoundedFinishes(t *testing.T) { clean(t, Bounded) }

func TestUnboundedWaitingIsEventuallyCritical(t *testing.T) { clean(t, Unbounded) }

func TestTurnBeforeFlagBreaksMutex(t *testing.T) {
	v := fv.Expect(t, TurnBeforeFlag, fv.Options{}, fv.KindInvariant, "mutual exclusion")
	if v.Steps() != 6 {
		t.Fatalf("want 6 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestWithoutFairnessAWaiterStarves(t *testing.T) {
	v := fv.Expect(t, NoFairness, fv.Options{}, fv.KindResponse, "waiting is eventually critical")
	if v.Trace.LoopsTo < 0 {
		t.Fatalf("want a lasso\n%s", v)
	}
	t.Logf("\n%s", v)
}

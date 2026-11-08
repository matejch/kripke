package posttrade

import (
	"testing"

	"github.com/matejch/kripke/fv"
	"github.com/matejch/kripke/purity"
)

func clean(t *testing.T, m fv.Model) fv.Result {
	t.Helper()
	r := fv.Check(t, m, fv.Options{})
	if !r.Complete || !r.CyclesChecked {
		t.Fatalf("incomplete: %s", fv.RenderResult(r))
	}
	return r
}

func TestFixedDesignIsCleanAndComplete(t *testing.T) {
	if r := clean(t, Fixed); r.States != 125337 {
		t.Fatalf("want 125337 states, got %d", r.States)
	}
}

func TestLegacyDoubleSettlesAcrossOverlappingRequests(t *testing.T) {
	v := fv.Expect(t, Legacy, fv.Options{}, fv.KindInvariant, "settled at most once")
	if v.Steps() != 6 {
		t.Fatalf("want batch, currency, two starts, two settles: 6 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestClaimAloneIsEnough(t *testing.T) { clean(t, NoStatusCheck) }

func TestStatusCheckAloneIsEnough(t *testing.T) { clean(t, NoClaim) }

func TestCrashWithoutReleaseStrandsClaimedTrades(t *testing.T) {
	v := fv.Expect(t, NoClaimRelease, fv.Options{}, fv.KindEventually, "every trade settled")
	if v.Steps() != 8 {
		t.Fatalf("want 8 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestCoreIsPure(t *testing.T) {
	purity.AssertPure(t, "../..", "./examples/posttrade/core")
}

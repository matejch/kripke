package settle

import (
	"testing"

	"github.com/matejch/kripke/fv"
	"github.com/matejch/kripke/purity"
)

func TestFixedDesignIsCleanAndComplete(t *testing.T) {
	r := fv.Check(t, Fixed, fv.Options{})
	if !r.Complete || !r.CyclesChecked {
		t.Fatalf("incomplete: %s", fv.RenderResult(r))
	}
	t.Log(fv.RenderResult(r))
}

func TestCrashAfterCommitDoubleSettlesWithoutIdempotency(t *testing.T) {
	v := fv.Expect(t, NoIdempotency, fv.Options{}, fv.KindInvariant, "settled at most once")
	if v.Steps() != 5 {
		t.Fatalf("want receive, commit, crash, receive, commit: 5 steps, got %d\n%s", v.Steps(), v)
	}
}

func TestCrashAfterEarlyAckLosesAnInstruction(t *testing.T) {
	v := fv.Expect(t, AckBeforeCommit, fv.Options{}, fv.KindEventually, "every instruction settled")
	if v.Steps() != 7 {
		t.Fatalf("want one instruction settled (4 steps) and the other received, acked and crashed (3), got %d\n%s", v.Steps(), v)
	}
}

func TestIdempotencyAloneDoesNotFixEarlyAck(t *testing.T) {
	r := fv.Run(AckBeforeCommit, fv.Options{ContinueAfterViolation: true})
	for _, v := range r.Violations {
		if v.Kind == fv.KindInvariant {
			t.Fatalf("idempotency is on here, duplicates must be impossible:\n%s", v)
		}
	}
	if len(r.Violations) == 0 {
		t.Fatal("early ack must still lose an instruction")
	}
}

func TestLateAckAloneDoesNotFixDuplicates(t *testing.T) {
	r := fv.Run(NoIdempotency, fv.Options{ContinueAfterViolation: true})
	for _, v := range r.Violations {
		if v.Kind != fv.KindInvariant {
			t.Fatalf("late ack never loses work:\n%s", v)
		}
	}
}

func TestLegacyEarlyAckMasksTheDuplicateBug(t *testing.T) {
	r := fv.Run(Legacy, fv.Options{ContinueAfterViolation: true})
	if len(r.Violations) != 1 || r.Violations[0].Kind != fv.KindEventually {
		t.Fatalf("legacy should only lose work; fixing the ack order is what exposes duplicates:\n%s", fv.RenderResult(r))
	}
}

func TestCoreIsPure(t *testing.T) {
	purity.AssertPure(t, "../..", "./examples/settle/core")
}

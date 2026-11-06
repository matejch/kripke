// Package twophase models two-phase commit with three interchangeable resource managers.
package twophase

import "github.com/matejch/kripke/fv"

type RM int

type RMState int

const (
	Working RMState = iota
	Prepared
	Committed
	Aborted
)

func (s RMState) String() string {
	return [...]string{"working", "prepared", "committed", "aborted"}[s]
}

type TMState int

const (
	TMInit TMState = iota
	TMCommitted
	TMAborted
)

func (s TMState) String() string { return [...]string{"init", "committed", "aborted"}[s] }

type Config struct {
	UnilateralAbort bool
}

type Participant struct {
	ID RM
	St RMState
}

type World struct {
	Cfg         Config        `fv:"-"`
	RMs         []Participant `fv:"set"`
	TM          TMState
	TMPrepared  []RM `fv:"set"`
	MsgPrepared []RM `fv:"set"`
	MsgCommit   bool
	MsgAbort    bool
}

func (w *World) Clone() fv.State {
	c := *w
	c.RMs = append([]Participant(nil), w.RMs...)
	c.TMPrepared = append([]RM(nil), w.TMPrepared...)
	c.MsgPrepared = append([]RM(nil), w.MsgPrepared...)
	return &c
}

func has(xs []RM, r RM) bool {
	for _, x := range xs {
		if x == r {
			return true
		}
	}
	return false
}

func initial(cfg Config) *World {
	return &World{Cfg: cfg, RMs: []Participant{{0, Working}, {1, Working}, {2, Working}}}
}

func tmRcvPrepared(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.TM != TMInit || len(w.MsgPrepared) == 0 {
		return nil
	}
	r := fv.Any(c, w.MsgPrepared)
	if has(w.TMPrepared, r) {
		return nil
	}
	w.TMPrepared = append(w.TMPrepared, r)
	return w
}

func tmCommit(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.TM != TMInit || len(w.TMPrepared) != len(w.RMs) {
		return nil
	}
	w.TM, w.MsgCommit = TMCommitted, true
	return w
}

func tmAbort(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.TM != TMInit {
		return nil
	}
	w.TM, w.MsgAbort = TMAborted, true
	return w
}

func rmPrepare(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	p := &w.RMs[c.Pick(len(w.RMs))]
	if p.St != Working {
		return nil
	}
	p.St = Prepared
	w.MsgPrepared = append(w.MsgPrepared, p.ID)
	return w
}

func rmChooseToAbort(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	p := &w.RMs[c.Pick(len(w.RMs))]
	if p.St != Working && !(w.Cfg.UnilateralAbort && p.St == Prepared) {
		return nil
	}
	p.St = Aborted
	return w
}

func rmRcvCommit(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	p := &w.RMs[c.Pick(len(w.RMs))]
	if !w.MsgCommit || p.St == Committed {
		return nil
	}
	p.St = Committed
	return w
}

func rmRcvAbort(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	p := &w.RMs[c.Pick(len(w.RMs))]
	if !w.MsgAbort || p.St == Aborted {
		return nil
	}
	p.St = Aborted
	return w
}

func consistent(s fv.State) bool {
	w := s.(*World)
	var committed, aborted bool
	for _, p := range w.RMs {
		committed = committed || p.St == Committed
		aborted = aborted || p.St == Aborted
	}
	return !(committed && aborted)
}

func decided(s fv.State) bool {
	for _, p := range s.(*World).RMs {
		if p.St != Committed && p.St != Aborted {
			return false
		}
	}
	return true
}

// Model builds the two-phase commit model for a configuration.
func Model(name string, cfg Config) fv.Model {
	return fv.Model{
		Name: name,
		Init: func() []fv.State { return []fv.State{initial(cfg)} },
		Actions: []fv.Action{
			{Name: "TMRcvPrepared", Step: tmRcvPrepared, Fair: true},
			{Name: "TMCommit", Step: tmCommit, Fair: true},
			{Name: "TMAbort", Step: tmAbort},
			{Name: "RMPrepare", Step: rmPrepare, Fair: true},
			{Name: "RMChooseToAbort", Step: rmChooseToAbort},
			{Name: "RMRcvCommit", Step: rmRcvCommit, Fair: true},
			{Name: "RMRcvAbort", Step: rmRcvAbort, Fair: true},
		},
		Invariants: []fv.Invariant{fv.Inv("consistent", consistent)},
		Eventually: []fv.Invariant{fv.Inv("all decided", decided)},
		Symmetries: []fv.Symmetry{fv.Sym[RM](0, 1, 2)},
	}
}

func with(cfg Config, f func(*Config)) Config {
	f(&cfg)
	return cfg
}

var (
	Fixed      = Model("twophase", Config{})
	Unilateral = Model("twophase-unilateral-abort", with(Config{}, func(c *Config) { c.UnilateralAbort = true }))
)

func init() {
	fv.Register(Fixed)
	fv.Register(Unilateral)
}

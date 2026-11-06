// Package settle models the world around core: an at-least-once queue, two workers and crashes.
package settle

import (
	"slices"

	"github.com/matej/kripke/examples/settle/core"
	"github.com/matej/kripke/fv"
)

type Config struct {
	NoIdempotency   bool
	AckBeforeCommit bool
}

type Phase int

const (
	Idle Phase = iota
	Received
	Acked
	Committed
)

func (p Phase) String() string { return [...]string{"idle", "received", "acked", "committed"}[p] }

type Msg struct {
	ID     int
	Leased bool
}

type Worker struct {
	Phase Phase
	Msg   core.Instruction
}

type World struct {
	Cfg     Config `fv:"-"`
	Queue   []Msg
	Workers [2]Worker
	Ledger  core.Ledger
	Crashes int
}

var instructions = []core.Instruction{{ID: 1, Amount: 100}, {ID: 2, Amount: 50}}

const crashBudget = 1

func (w *World) Clone() fv.State {
	c := *w
	c.Queue = slices.Clone(w.Queue)
	c.Ledger = w.Ledger.Clone()
	return &c
}

func (w *World) policy() core.Policy { return core.Policy{Idempotent: !w.Cfg.NoIdempotency} }

func (w *World) ack(id int) {
	w.Queue = slices.DeleteFunc(w.Queue, func(m Msg) bool { return m.ID == id })
}

func (w *World) release(id int) {
	for i := range w.Queue {
		if w.Queue[i].ID == id {
			w.Queue[i].Leased = false
		}
	}
}

func initial(cfg Config) *World {
	w := &World{Cfg: cfg}
	for _, in := range instructions {
		w.Queue = append(w.Queue, Msg{ID: in.ID})
	}
	return w
}

func receive(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.Workers[i].Phase != Idle || len(w.Queue) == 0 {
			return nil
		}
		m := &w.Queue[c.Pick(len(w.Queue))]
		if m.Leased {
			return nil
		}
		m.Leased = true
		w.Workers[i] = Worker{Phase: Received, Msg: instructions[m.ID-1]}
		return w
	}
}

func ackEarly(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		wk := &w.Workers[i]
		if !w.Cfg.AckBeforeCommit || wk.Phase != Received {
			return nil
		}
		w.ack(wk.Msg.ID)
		wk.Phase = Acked
		return w
	}
}

func commit(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		wk := &w.Workers[i]
		ready := Received
		if w.Cfg.AckBeforeCommit {
			ready = Acked
		}
		if wk.Phase != ready {
			return nil
		}
		if l, err := w.Ledger.Settle(wk.Msg, w.policy()); err == nil {
			w.Ledger = l
		}
		wk.Phase = Committed
		return w
	}
}

func ack(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		wk := &w.Workers[i]
		if wk.Phase != Committed {
			return nil
		}
		w.ack(wk.Msg.ID)
		*wk = Worker{}
		return w
	}
}

func crash(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		wk := &w.Workers[i]
		if wk.Phase == Idle || w.Crashes >= crashBudget {
			return nil
		}
		w.release(wk.Msg.ID)
		*wk = Worker{}
		w.Crashes++
		return w
	}
}

func settledOnce(s fv.State) bool {
	w := s.(*World)
	for _, in := range instructions {
		if w.Ledger.Count(in.ID) > 1 {
			return false
		}
	}
	return true
}

func allSettled(s fv.State) bool {
	w := s.(*World)
	for _, in := range instructions {
		if w.Ledger.Count(in.ID) == 0 {
			return false
		}
	}
	return true
}

// Model builds the settlement world; all toggles false is the fixed design.
func Model(name string, cfg Config) fv.Model {
	m := fv.Model{
		Name:       name,
		Init:       func() []fv.State { return []fv.State{initial(cfg)} },
		Invariants: []fv.Invariant{fv.Inv("settled at most once", settledOnce)},
		Eventually: []fv.Invariant{fv.Inv("every instruction settled", allSettled)},
	}
	for i := range 2 {
		n := string(rune('0' + i))
		m.Actions = append(m.Actions,
			fv.Action{Name: "receive" + n, Step: receive(i), Fair: true},
			fv.Action{Name: "ackEarly" + n, Step: ackEarly(i), Fair: true},
			fv.Action{Name: "commit" + n, Step: commit(i), Fair: true},
			fv.Action{Name: "ack" + n, Step: ack(i), Fair: true},
			fv.Action{Name: "crash" + n, Step: crash(i)},
		)
	}
	return m
}

func with(cfg Config, f func(*Config)) Config {
	f(&cfg)
	return cfg
}

var (
	Fixed           = Model("settle", Config{})
	NoIdempotency   = Model("settle-no-idempotency", with(Config{}, func(c *Config) { c.NoIdempotency = true }))
	AckBeforeCommit = Model("settle-ack-before-commit", with(Config{}, func(c *Config) { c.AckBeforeCommit = true }))
	Legacy          = Model("settle-legacy", Config{NoIdempotency: true, AckBeforeCommit: true})
)

func init() {
	fv.Register(Fixed)
	fv.Register(NoIdempotency)
	fv.Register(AckBeforeCommit)
	fv.Register(Legacy)
}

// Package transfer models two workers moving money between three interchangeable accounts.
package transfer

import (
	"slices"

	"github.com/matejch/kripke/fv"
)

type Acct int

type Config struct {
	NonAtomic bool
}

type Account struct {
	ID  Acct
	Bal int
}

type Phase int

const (
	Ready Phase = iota
	Read
)

type Worker struct {
	Phase    Phase
	From, To Acct
	SeenFrom int
	SeenTo   int
	Left     int
}

type World struct {
	Cfg      Config    `fv:"-"`
	Accounts []Account `fv:"set"`
	Workers  []Worker  `fv:"set"`
}

const (
	start  = 1
	rounds = 2
)

func (w *World) Clone() fv.State {
	c := *w
	c.Accounts = slices.Clone(w.Accounts)
	c.Workers = slices.Clone(w.Workers)
	return &c
}

func (w *World) acct(id Acct) *Account {
	for i := range w.Accounts {
		if w.Accounts[i].ID == id {
			return &w.Accounts[i]
		}
	}
	panic("no account")
}

func initial(cfg Config) *World {
	return &World{
		Cfg:      cfg,
		Accounts: []Account{{0, start}, {1, start}, {2, start}},
		Workers:  []Worker{{Left: rounds}, {Left: rounds}},
	}
}

func pickPair(c *fv.Choice, w *World) (Acct, Acct) {
	from := w.Accounts[c.Pick(len(w.Accounts))].ID
	to := w.Accounts[c.Pick(len(w.Accounts))].ID
	return from, to
}

func begin(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	wk := &w.Workers[c.Pick(len(w.Workers))]
	if wk.Phase != Ready || wk.Left == 0 {
		return nil
	}
	from, to := pickPair(c, w)
	if from == to || w.acct(from).Bal == 0 {
		return nil
	}
	wk.Left--
	if !w.Cfg.NonAtomic {
		w.acct(from).Bal--
		w.acct(to).Bal++
		return w
	}
	wk.Phase, wk.From, wk.To = Read, from, to
	wk.SeenFrom, wk.SeenTo = w.acct(from).Bal, w.acct(to).Bal
	return w
}

func write(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	wk := &w.Workers[c.Pick(len(w.Workers))]
	if wk.Phase != Read {
		return nil
	}
	w.acct(wk.From).Bal = wk.SeenFrom - 1
	w.acct(wk.To).Bal = wk.SeenTo + 1
	*wk = Worker{Left: wk.Left}
	return w
}

func conserved(s fv.State) bool {
	total := 0
	for _, a := range s.(*World).Accounts {
		total += a.Bal
	}
	return total == 3*start
}

// Model builds the transfer model.
func Model(name string, cfg Config) fv.Model {
	return fv.Model{
		Name: name,
		Init: func() []fv.State { return []fv.State{initial(cfg)} },
		Actions: []fv.Action{
			{Name: "begin", Step: begin},
			{Name: "write", Step: write, Fair: true},
		},
		Invariants: []fv.Invariant{fv.Inv("money conserved", conserved)},
		Symmetries: []fv.Symmetry{fv.Sym[Acct](0, 1, 2)},
	}
}

var (
	Atomic    = Model("transfer", Config{})
	NonAtomic = Model("transfer-non-atomic", Config{NonAtomic: true})
)

func init() {
	fv.Register(Atomic)
	fv.Register(NonAtomic)
}

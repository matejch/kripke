// Package posttrade models settlement requests from clients and the end-of-day batch racing over one trade book.
package posttrade

import (
	"slices"

	"github.com/matejch/kripke/examples/posttrade/core"
	"github.com/matejch/kripke/fv"
)

type Config struct {
	NoClaim        bool
	NoStatusCheck  bool
	NoClaimRelease bool
}

type Phase int

const (
	Queued Phase = iota
	Working
	Done
)

func (p Phase) String() string { return [...]string{"queued", "working", "done"}[p] }

type Job struct {
	ID    int
	Scope core.Scope
	Phase Phase
	Todo  []int
}

type World struct {
	Cfg       Config `fv:"-"`
	Book      core.Book
	Jobs      []Job
	BatchSent bool
	CcySent   bool
	EODRun    bool
	Crashes   int
}

const crashBudget = 1

var currencies = []core.Ccy{"EUR", "USD"}

func (w *World) Clone() fv.State {
	c := *w
	c.Book = w.Book.Clone()
	c.Jobs = slices.Clone(w.Jobs)
	for i := range c.Jobs {
		c.Jobs[i].Todo = slices.Clone(w.Jobs[i].Todo)
		c.Jobs[i].Scope.IDs = slices.Clone(w.Jobs[i].Scope.IDs)
	}
	return &c
}

func (w *World) policy() core.Policy {
	return core.Policy{Claim: !w.Cfg.NoClaim, CheckStatus: !w.Cfg.NoStatusCheck}
}

func (w *World) submit(s core.Scope) {
	w.Jobs = append(w.Jobs, Job{ID: len(w.Jobs) + 1, Scope: s})
}

func initial(cfg Config) *World {
	return &World{Cfg: cfg, Book: core.Book{Trades: []core.Trade{
		{ID: 1, Ccy: "EUR"}, {ID: 2, Ccy: "EUR"}, {ID: 3, Ccy: "USD"}, {ID: 4, Ccy: "USD"},
	}}}
}

func clientBatch(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.BatchSent {
		return nil
	}
	var ids []int
	for _, t := range w.Book.Trades {
		if c.Bool() {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	w.BatchSent = true
	w.submit(core.Scope{Kind: core.Batch, IDs: ids})
	return w
}

func clientCurrency(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.CcySent {
		return nil
	}
	w.CcySent = true
	w.submit(core.Scope{Kind: core.Currency, Ccy: fv.Any(c, currencies)})
	return w
}

func endOfDay(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.EODRun {
		return nil
	}
	w.EODRun = true
	w.submit(core.Scope{Kind: core.EndOfDay})
	return w
}

func start(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if len(w.Jobs) == 0 {
		return nil
	}
	j := &w.Jobs[c.Pick(len(w.Jobs))]
	if j.Phase != Queued {
		return nil
	}
	ids := w.Book.Select(j.Scope)
	if !w.Cfg.NoClaim {
		w.Book, ids = w.Book.Claim(j.ID, ids)
	}
	j.Phase, j.Todo = Working, ids
	if len(ids) == 0 {
		j.Phase = Done
	}
	return w
}

func settle(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if len(w.Jobs) == 0 {
		return nil
	}
	j := &w.Jobs[c.Pick(len(w.Jobs))]
	if j.Phase != Working {
		return nil
	}
	if b, err := w.Book.Settle(j.ID, j.Todo[0], w.policy()); err == nil {
		w.Book = b
	}
	j.Todo = j.Todo[1:]
	if len(j.Todo) == 0 {
		j.Phase, j.Todo = Done, nil
	}
	return w
}

func crash(c *fv.Choice, s fv.State) fv.State {
	w := s.(*World)
	if w.Crashes >= crashBudget || len(w.Jobs) == 0 {
		return nil
	}
	j := &w.Jobs[c.Pick(len(w.Jobs))]
	if j.Phase != Working {
		return nil
	}
	if !w.Cfg.NoClaimRelease {
		w.Book = w.Book.Release(j.ID)
	}
	j.Phase, j.Todo = Queued, nil
	w.Crashes++
	return w
}

func settledOnce(s fv.State) bool {
	w := s.(*World)
	for _, t := range w.Book.Trades {
		if w.Book.Posted(t.ID) > 1 {
			return false
		}
	}
	return true
}

func postingsInScope(s fv.State) bool {
	w := s.(*World)
	for _, p := range w.Book.Postings {
		t := w.Book.Trades[slices.IndexFunc(w.Book.Trades, func(t core.Trade) bool { return t.ID == p.Trade })]
		if !w.Jobs[p.Job-1].Scope.Covers(t) {
			return false
		}
	}
	return true
}

func statusMatchesPostings(s fv.State) bool {
	w := s.(*World)
	for _, t := range w.Book.Trades {
		if (t.Status == core.Settled) != (w.Book.Posted(t.ID) > 0) {
			return false
		}
	}
	return true
}

func allSettled(s fv.State) bool {
	for _, t := range s.(*World).Book.Trades {
		if t.Status != core.Settled {
			return false
		}
	}
	return true
}

// Model builds the post-trade world; all toggles false is the fixed design.
func Model(name string, cfg Config) fv.Model {
	return fv.Model{
		Name: name,
		Init: func() []fv.State { return []fv.State{initial(cfg)} },
		Actions: []fv.Action{
			{Name: "clientBatch", Step: clientBatch},
			{Name: "clientCurrency", Step: clientCurrency},
			{Name: "endOfDay", Step: endOfDay, Fair: true},
			{Name: "start", Step: start, Fair: true},
			{Name: "settle", Step: settle, Fair: true},
			{Name: "crash", Step: crash},
		},
		Invariants: []fv.Invariant{
			fv.Inv("settled at most once", settledOnce),
			fv.Inv("postings stay in scope", postingsInScope),
			fv.Inv("status matches postings", statusMatchesPostings),
		},
		Eventually: []fv.Invariant{fv.Inv("every trade settled", allSettled)},
	}
}

var (
	Fixed          = Model("posttrade", Config{})
	NoClaim        = Model("posttrade-no-claim", Config{NoClaim: true})
	NoStatusCheck  = Model("posttrade-no-status-check", Config{NoStatusCheck: true})
	Legacy         = Model("posttrade-legacy", Config{NoClaim: true, NoStatusCheck: true})
	NoClaimRelease = Model("posttrade-no-claim-release", Config{NoClaimRelease: true})
)

func init() {
	fv.Register(Fixed)
	fv.Register(NoClaim)
	fv.Register(NoStatusCheck)
	fv.Register(Legacy)
	fv.Register(NoClaimRelease)
}

// Package philosophers models three dining philosophers sharing three forks.
package philosophers

import "github.com/matejch/kripke/fv"

const N = 3

type Phase int

const (
	Thinking Phase = iota
	Hungry
	HasFirst
	Eating
)

func (p Phase) String() string { return [...]string{"thinking", "hungry", "has-first", "eating"}[p] }

type Config struct {
	Naive    bool
	Meals    int
	WeakOnly bool
}

type World struct {
	Cfg   Config `fv:"-"`
	Phase [N]Phase
	Fork  [N]int
	Ate   [N]int
}

func (w *World) Clone() fv.State { c := *w; return &c }

func forks(cfg Config, i int) (first, second int) {
	left, right := i, (i+1)%N
	if !cfg.Naive && i == N-1 {
		return right, left
	}
	return left, right
}

func initial(cfg Config) *World {
	w := &World{Cfg: cfg}
	for i := range w.Fork {
		w.Fork[i] = -1
	}
	return w
}

func hungry(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.Phase[i] != Thinking || (w.Cfg.Meals > 0 && w.Ate[i] >= w.Cfg.Meals) {
			return nil
		}
		w.Phase[i] = Hungry
		return w
	}
}

func take(i int, phase Phase) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.Phase[i] != phase {
			return nil
		}
		f, g := forks(w.Cfg, i)
		if phase == HasFirst {
			f = g
		}
		if w.Fork[f] != -1 {
			return nil
		}
		w.Fork[f] = i
		w.Phase[i]++
		return w
	}
}

func release(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.Phase[i] != Eating {
			return nil
		}
		f, g := forks(w.Cfg, i)
		w.Fork[f], w.Fork[g], w.Phase[i] = -1, -1, Thinking
		if w.Cfg.Meals > 0 {
			w.Ate[i]++
		}
		return w
	}
}

// Model builds the philosophers; Meals > 0 bounds the run, 0 lets them eat forever.
func Model(name string, cfg Config) fv.Model {
	m := fv.Model{Name: name, Init: func() []fv.State { return []fv.State{initial(cfg)} }}
	strong := !cfg.WeakOnly
	for i := range N {
		n := string(rune('0' + i))
		m.Actions = append(m.Actions,
			fv.Action{Name: "hungry" + n, Step: hungry(i)},
			fv.Action{Name: "takeFirst" + n, Step: take(i, Hungry), Fair: true, StrongFair: strong},
			fv.Action{Name: "takeSecond" + n, Step: take(i, HasFirst), Fair: true, StrongFair: strong},
			fv.Action{Name: "release" + n, Step: release(i), Fair: true},
		)
	}
	m.Invariants = []fv.Invariant{fv.Inv("fork held by its user", func(s fv.State) bool {
		w := s.(*World)
		for i := range N {
			f, g := forks(w.Cfg, i)
			if w.Phase[i] >= HasFirst && w.Fork[f] != i || w.Phase[i] == Eating && w.Fork[g] != i {
				return false
			}
		}
		return true
	})}
	if cfg.Meals > 0 {
		m.Eventually = []fv.Invariant{fv.Inv("everyone ate", func(s fv.State) bool {
			for _, a := range s.(*World).Ate {
				if a < cfg.Meals {
					return false
				}
			}
			return true
		})}
	} else {
		m.DeadlockIsError = true
		m.Responses = []fv.Response{fv.Resp("hungry 0 eventually eats",
			func(s fv.State) bool { return s.(*World).Phase[0] == Hungry },
			func(s fv.State) bool { return s.(*World).Phase[0] == Eating })}
	}
	return m
}

var (
	Polite       = Model("philosophers", Config{Meals: 1})
	Naive        = Model("philosophers-naive", Config{Meals: 1, Naive: true})
	Forever      = Model("philosophers-forever", Config{})
	ForeverNaive = Model("philosophers-forever-naive", Config{Naive: true})
	ForeverWeak  = Model("philosophers-forever-weak-fairness", Config{WeakOnly: true})
)

func init() {
	fv.Register(Polite)
	fv.Register(Naive)
	fv.Register(Forever)
	fv.Register(ForeverNaive)
	fv.Register(ForeverWeak)
}

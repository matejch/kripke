// Package peterson models Peterson's two-process mutual exclusion.
package peterson

import "github.com/matej/kripke/fv"

type PC int

const (
	Idle PC = iota
	SetTurn
	SetFlag
	Wait
	Crit
)

func (p PC) String() string { return [...]string{"idle", "set-turn", "set-flag", "wait", "crit"}[p] }

type Config struct {
	TurnBeforeFlag bool
	NoFairness     bool
	Rounds         int
}

type World struct {
	Cfg  Config `fv:"-"`
	PC   [2]PC
	Flag [2]bool
	Turn int
	Done [2]int
}

func (w *World) Clone() fv.State { c := *w; return &c }

func request(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.PC[i] != Idle || (w.Cfg.Rounds > 0 && w.Done[i] >= w.Cfg.Rounds) {
			return nil
		}
		if w.Cfg.TurnBeforeFlag {
			w.Turn, w.PC[i] = 1-i, SetFlag
		} else {
			w.Flag[i], w.PC[i] = true, SetTurn
		}
		return w
	}
}

func second(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		switch w.PC[i] {
		case SetTurn:
			w.Turn = 1 - i
		case SetFlag:
			w.Flag[i] = true
		default:
			return nil
		}
		w.PC[i] = Wait
		return w
	}
}

func enter(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.PC[i] != Wait || (w.Flag[1-i] && w.Turn != i) {
			return nil
		}
		w.PC[i] = Crit
		return w
	}
}

func exit(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.PC[i] != Crit {
			return nil
		}
		w.PC[i], w.Flag[i] = Idle, false
		if w.Cfg.Rounds > 0 {
			w.Done[i]++
		}
		return w
	}
}

func think(i int) func(*fv.Choice, fv.State) fv.State {
	return func(c *fv.Choice, s fv.State) fv.State {
		w := s.(*World)
		if w.PC[i] != Idle || w.Cfg.Rounds > 0 {
			return nil
		}
		return w
	}
}

// Model builds Peterson's algorithm; Rounds > 0 bounds each process, 0 lets them loop forever.
func Model(name string, cfg Config) fv.Model {
	m := fv.Model{
		Name: name,
		Init: func() []fv.State { return []fv.State{&World{Cfg: cfg}} },
		Invariants: []fv.Invariant{fv.Inv("mutual exclusion", func(s fv.State) bool {
			w := s.(*World)
			return !(w.PC[0] == Crit && w.PC[1] == Crit)
		})},
	}
	fair := !cfg.NoFairness
	for i := range 2 {
		n := string(rune('0' + i))
		m.Actions = append(m.Actions,
			fv.Action{Name: "request" + n, Step: request(i)},
			fv.Action{Name: "think" + n, Step: think(i)},
			fv.Action{Name: "second" + n, Step: second(i), Fair: fair},
			fv.Action{Name: "enter" + n, Step: enter(i), Fair: fair},
			fv.Action{Name: "exit" + n, Step: exit(i), Fair: fair},
		)
	}
	if cfg.Rounds > 0 {
		m.Eventually = []fv.Invariant{fv.Inv("all rounds done", func(s fv.State) bool {
			w := s.(*World)
			return w.Done[0] == cfg.Rounds && w.Done[1] == cfg.Rounds
		})}
	} else {
		m.Responses = []fv.Response{fv.Resp("waiting is eventually critical",
			func(s fv.State) bool { return s.(*World).PC[0] == Wait },
			func(s fv.State) bool { return s.(*World).PC[0] == Crit })}
	}
	return m
}

func with(cfg Config, f func(*Config)) Config {
	f(&cfg)
	return cfg
}

var (
	Bounded        = Model("peterson-bounded", Config{Rounds: 2})
	Unbounded      = Model("peterson", Config{})
	TurnBeforeFlag = Model("peterson-turn-before-flag", with(Config{}, func(c *Config) { c.TurnBeforeFlag = true }))
	NoFairness     = Model("peterson-no-fairness", with(Config{}, func(c *Config) { c.NoFairness = true }))
)

func init() {
	fv.Register(Bounded)
	fv.Register(Unbounded)
	fv.Register(TurnBeforeFlag)
	fv.Register(NoFairness)
}

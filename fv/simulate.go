package fv

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
)

type walk struct {
	keys  []stateKey
	steps int
	depth int
	fail  *Violation
	path  []walkStep
}

type walkStep struct {
	act     int
	choices []int32
	init    int
}

func simulate(m *Model, o Options, res *Result) {
	depth := o.MaxDepth
	if depth <= 0 {
		depth = 100
	}
	workers := o.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	x := &explorer{m: m, o: o, k: newKeyer(m, o), res: res}
	walks := make([]walk, o.Simulate)
	var wg sync.WaitGroup
	next := make(chan int)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := x.newWorker()
			for i := range next {
				walks[i] = w.walk(o.Seed, i, depth)
			}
		}()
	}
	for i := range o.Simulate {
		next <- i
	}
	close(next)
	wg.Wait()
	seen := map[stateKey]bool{}
	reported := map[string]bool{}
	for i := range walks {
		wk := &walks[i]
		for _, k := range wk.keys {
			seen[k] = true
		}
		res.Transitions += wk.steps
		res.Depth = max(res.Depth, wk.depth)
		if wk.fail != nil && !reported[wk.fail.Kind+"\x00"+wk.fail.Name] && (o.ContinueAfterViolation || len(res.Violations) == 0) {
			reported[wk.fail.Kind+"\x00"+wk.fail.Name] = true
			wk.fail.Trace = x.replayWalk(wk.path)
			wk.fail.Detail = fmt.Sprintf("random walk %d (seed %d)", i, o.Seed) + suffix(wk.fail.Detail)
			res.Violations = append(res.Violations, *wk.fail)
		}
	}
	res.States = len(seen)
	res.Simulated = o.Simulate
	res.Incomplete = "simulation"
}

func suffix(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

func (w *worker) walk(seed uint64, i, depth int) (wk walk) {
	x := w.x
	det := !x.o.NoDeterminismCheck
	rng := rand.New(rand.NewPCG(seed, uint64(i)))
	defer func() {
		if r := recover(); r != nil {
			f := w.failFrom(r)
			wk.fail = &Violation{Kind: f.kind, Name: f.name, Detail: f.detail}
		}
	}()
	inits := x.m.Init()
	ii := rng.IntN(len(inits))
	st := inits[ii]
	wk.path = append(wk.path, walkStep{init: ii, act: -1})
	cur := succ{st: st, key: w.k.canonical(st)}
	w.eval(&cur)
	for d := 0; ; d++ {
		wk.keys = append(wk.keys, cur.key)
		wk.depth = d
		if len(cur.bad) > 0 {
			wk.fail = &Violation{Kind: KindInvariant, Name: x.m.Invariants[cur.bad[0]].Name}
			return wk
		}
		if d == depth {
			return wk
		}
		praw := stateKey{}
		if det {
			praw = w.k.raw(cur.st)
		}
		var all []succ
		for a := range x.m.Actions {
			all = append(all, w.runAction(cur.st, a, det, praw, nil)...)
		}
		if len(all) == 0 {
			if x.m.DeadlockIsError {
				wk.fail = &Violation{Kind: KindDeadlock, Name: "deadlock"}
				return wk
			}
			for j, ev := range x.m.Eventually {
				if cur.props&(1<<j) == 0 {
					wk.fail = &Violation{Kind: KindEventually, Name: ev.Name}
					return wk
				}
			}
			return wk
		}
		cur = all[rng.IntN(len(all))]
		wk.steps++
		wk.path = append(wk.path, walkStep{act: int(cur.act), choices: cur.choices})
	}
}

func (x *explorer) replayWalk(path []walkStep) Trace {
	w := x.newWorker()
	tr := Trace{LoopsTo: -1}
	var st State
	for _, p := range path {
		if p.act < 0 {
			st = x.m.Init()[p.init]
			tr.Steps = append(tr.Steps, Step{Action: "init", State: flatten(st)})
			continue
		}
		act := &x.m.Actions[p.act]
		w.c.reset(p.choices, true)
		st = act.Step(&w.c, st.Clone())
		if st == nil {
			break
		}
		tr.Steps = append(tr.Steps, Step{Action: act.Name, Choices: toInts(p.choices), State: flatten(st)})
	}
	return tr
}

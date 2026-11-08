package fv

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type node struct {
	parent    int32
	action    int16
	cLen      int16
	depth     int32
	cOff      int32
	key       stateKey
	props     uint64
	enabled   uint64
	resp      uint64
	quiescent bool
}

type edge struct {
	from, to int32
	act      int16
}

type succ struct {
	st          State
	key         stateKey
	act         int16
	choices     []int32
	props, resp uint64
	bad         []int
}

type failure struct {
	kind, name, detail string
}

type expansion struct {
	succs   []succ
	enabled uint64
	fail    *failure
	comp    int16
}

type item struct {
	idx  int32
	st   State
	comp int16
}

type explorer struct {
	m         *Model
	o         Options
	k         *keyer
	nodes     []node
	seen      map[stateKey]int32
	arena     []int32
	edges     []edge
	keepEdges bool
	res       *Result
	reported  map[string]bool
	halt      atomic.Bool
	stopped   bool
	limited   []string
	start     time.Time
	por       *porInfo
	pending   []item
	workers   int
	allActs   []int
}

type worker struct {
	x     *explorer
	k     *keyer
	c     Choice
	where string
	per   [][]succ
	ran   []bool
}

// Run explores the model and returns the result; it never panics on model errors.
func Run(m Model, o Options) (res Result) {
	start := time.Now()
	res.Model = m.Name
	defer func() {
		if r := recover(); r != nil {
			res.Error = fmt.Sprint(r)
		}
		res.Elapsed = time.Since(start)
	}()
	if m.Init == nil {
		res.Error = fmt.Sprintf("model %s: Init is nil", m.Name)
		return res
	}
	inits := m.Init()
	if len(inits) == 0 {
		res.Error = fmt.Sprintf("model %s: Init returned no states", m.Name)
		return res
	}
	if len(inits) > maxInits {
		res.Error = fmt.Sprintf("model %s: Init returned %d states, the limit is %d", m.Name, len(inits), maxInits)
		return res
	}
	if err := m.validate(o, inits[0]); err != nil {
		res.Error = err.Error()
		return res
	}
	res.Warnings = hygiene(inits[0])
	if o.Simulate > 0 {
		simulate(&m, o, &res)
		return res
	}
	x := &explorer{
		m:         &m,
		o:         o,
		k:         newKeyer(&m, o),
		seen:      map[stateKey]int32{},
		keepEdges: len(m.Eventually) > 0 || len(m.Responses) > 0,
		res:       &res,
		reported:  map[string]bool{},
		start:     start,
		workers:   o.Workers,
	}
	for i := range m.Actions {
		x.allActs = append(x.allActs, i)
	}
	if x.workers <= 0 {
		x.workers = runtime.GOMAXPROCS(0)
	}
	if o.DFS {
		x.workers = 1
	}
	if o.POR {
		x.por = newPOR(&m)
	}
	layer := x.initial(inits)
	if o.DFS {
		x.dfs(layer)
	} else {
		x.bfs(layer)
	}
	res.States = len(x.nodes)
	res.Complete = !x.stopped && len(x.limited) == 0
	if !res.Complete {
		why := slices.Clone(x.limited)
		if x.stopped {
			why = append(why, "stopped at first violation")
		}
		res.Incomplete = strings.Join(why, ", ")
	}
	if res.Complete {
		res.CyclesChecked = true
		x.liveness()
	}
	return res
}

func (x *explorer) initial(inits []State) []item {
	var layer []item
	w := x.newWorker()
	for i, s := range inits {
		key := x.k.canonical(s)
		if _, ok := x.seen[key]; ok {
			continue
		}
		sc := succ{st: s, key: key, act: int16(i)}
		if f := w.evalSafe(&sc); f != nil {
			x.res.Violations = append(x.res.Violations, Violation{Kind: f.kind, Name: f.name, Detail: f.detail, Trace: Trace{LoopsTo: -1}})
			x.stopped = true
			return nil
		}
		id := x.add(-1, sc, 0)
		layer = append(layer, item{idx: id, st: s})
		x.checkNew(id, sc)
	}
	return layer
}

func (x *explorer) add(parent int32, s succ, depth int32) int32 {
	if len(x.nodes) >= maxNodes {
		panic(fmt.Sprintf("fv: more than %d states; the node table is indexed by int32", maxNodes))
	}
	if len(s.choices) > maxChoiceLen {
		panic(fmt.Sprintf("fv: action %s recorded %d choices in one step, the limit is %d", x.m.Actions[s.act].Name, len(s.choices), maxChoiceLen))
	}
	if len(x.arena)+len(s.choices) > maxNodes {
		panic("fv: choice arena exceeds int32 offsets")
	}
	id := int32(len(x.nodes))
	x.nodes = append(x.nodes, node{
		parent: parent, action: s.act, cLen: int16(len(s.choices)), depth: depth,
		cOff: int32(len(x.arena)), key: s.key, props: s.props, resp: s.resp,
	})
	x.arena = append(x.arena, s.choices...)
	x.seen[s.key] = id
	if int(depth) > x.res.Depth {
		x.res.Depth = int(depth)
	}
	return id
}

func (x *explorer) checkNew(id int32, s succ) {
	for _, i := range s.bad {
		x.report(KindInvariant, x.m.Invariants[i].Name, "", x.path(id))
	}
}

func (x *explorer) report(kind, name, detail string, tr func() Trace) {
	k := kind + "\x00" + name
	if x.reported[k] {
		return
	}
	x.reported[k] = true
	x.res.Violations = append(x.res.Violations, Violation{Kind: kind, Name: name, Detail: detail, Trace: tr()})
	if !x.o.ContinueAfterViolation {
		x.stopped = true
		x.halt.Store(true)
	}
}

func (x *explorer) path(id int32) func() Trace { return func() Trace { return x.pathTo(id) } }

func (x *explorer) limit(why string) {
	if !slices.Contains(x.limited, why) {
		x.limited = append(x.limited, why)
	}
}

func (x *explorer) bfs(layer []item) {
	for depth := 0; len(layer) > 0 && !x.halt.Load(); depth++ {
		next := x.pass(layer, func(it item, ex *expansion, out *[]item) { x.merge(it, ex, out) })
		if len(x.pending) > 0 && !x.halt.Load() {
			rest := x.pending
			x.pending = nil
			next = append(next, x.pass(rest, func(it item, ex *expansion, out *[]item) { x.merge(it, ex, out) })...)
		}
		layer = next
		if x.o.Progress != nil {
			x.o.Progress(Progress{Depth: depth + 1, States: len(x.nodes), Frontier: len(layer), Elapsed: time.Since(x.start)})
		}
	}
}

func (x *explorer) pass(layer []item, merge func(item, *expansion, *[]item)) []item {
	const chunk = 128
	n := (len(layer) + chunk - 1) / chunk
	results := make([][]expansion, n)
	done := make([]chan struct{}, n)
	for i := range done {
		done[i] = make(chan struct{})
	}
	jobs := make(chan int)
	window := make(chan struct{}, 4*x.workers)
	var wg sync.WaitGroup
	for range x.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := x.newWorker()
			for j := range jobs {
				if !x.halt.Load() {
					lo, hi := j*chunk, min((j+1)*chunk, len(layer))
					out := make([]expansion, hi-lo)
					for i := lo; i < hi; i++ {
						out[i-lo] = w.expand(layer[i])
					}
					results[j] = out
				}
				close(done[j])
			}
		}()
	}
	go func() {
		for j := range n {
			window <- struct{}{}
			jobs <- j
		}
		close(jobs)
	}()
	var next []item
	for j := range n {
		<-done[j]
		if !x.halt.Load() {
			lo := j * chunk
			for i, ex := range results[j] {
				merge(layer[lo+i], &ex, &next)
				layer[lo+i].st = nil
				if x.halt.Load() {
					break
				}
			}
		}
		results[j] = nil
		if x.o.Deadline > 0 && time.Since(x.start) > x.o.Deadline && !x.halt.Load() {
			x.limit("deadline")
			x.halt.Store(true)
		}
		<-window
	}
	wg.Wait()
	return next
}

func (x *explorer) dfs(stack []item) {
	slices.Reverse(stack)
	w := x.newWorker()
	for len(stack) > 0 && !x.halt.Load() {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ex := w.expand(it)
		var next []item
		x.merge(it, &ex, &next)
		slices.Reverse(next)
		stack = append(stack, next...)
		if x.o.Deadline > 0 && time.Since(x.start) > x.o.Deadline {
			x.limit("deadline")
			return
		}
	}
}

func (x *explorer) merge(it item, ex *expansion, next *[]item) {
	n := &x.nodes[it.idx]
	n.enabled |= ex.enabled
	if ex.fail != nil {
		x.report(ex.fail.kind, ex.fail.name, ex.fail.detail, x.path(it.idx))
		x.limit("a state's expansion failed and its successors were not explored")
		return
	}
	if len(ex.succs) == 0 && it.comp == 0 {
		n.quiescent = true
		if x.m.DeadlockIsError {
			x.report(KindDeadlock, "deadlock", "no action is enabled", x.path(it.idx))
		}
		for i, ev := range x.m.Eventually {
			if n.props&(1<<i) == 0 {
				x.report(KindEventually, ev.Name, "quiescent state where it does not hold", x.path(it.idx))
			}
		}
	}
	depth := x.nodes[it.idx].depth
	capped := x.o.MaxDepth > 0 && int(depth) >= x.o.MaxDepth
	hit := false
	for _, s := range ex.succs {
		x.res.Transitions++
		if id, ok := x.seen[s.key]; ok {
			hit = true
			if x.keepEdges {
				x.edges = append(x.edges, edge{it.idx, id, s.act})
			}
			continue
		}
		if capped {
			x.limit(fmt.Sprintf("max depth %d", x.o.MaxDepth))
			continue
		}
		if x.o.MaxStates > 0 && len(x.nodes) >= x.o.MaxStates {
			x.limit(fmt.Sprintf("max states %d", x.o.MaxStates))
			continue
		}
		id := x.add(it.idx, s, depth+1)
		if x.keepEdges {
			x.edges = append(x.edges, edge{it.idx, id, s.act})
		}
		*next = append(*next, item{idx: id, st: s.st})
		x.checkNew(id, s)
		if x.halt.Load() {
			return
		}
	}
	if ex.comp > 0 && hit {
		x.pending = append(x.pending, item{idx: it.idx, st: it.st, comp: ex.comp})
	}
}

func (x *explorer) newWorker() *worker {
	return &worker{x: x, k: newKeyer(x.m, x.o)}
}

func (w *worker) expand(it item) (ex expansion) {
	defer func() {
		if r := recover(); r != nil {
			ex.fail = w.failFrom(r)
		}
	}()
	x := w.x
	det := !x.o.NoDeterminismCheck
	var praw stateKey
	if det {
		w.where = "encoding"
		praw = w.k.raw(it.st)
	}
	if w.per == nil {
		w.per, w.ran = make([][]succ, len(x.m.Actions)), make([]bool, len(x.m.Actions))
	}
	per, ran := w.per, w.ran
	clear(ran)
	run := func(a int) []succ {
		if !ran[a] {
			ran[a] = true
			per[a] = w.runAction(it.st, a, det, praw, per[a][:0])
			if len(per[a]) > 0 {
				ex.enabled |= 1 << a
			}
		}
		return per[a]
	}
	var acts []int
	switch {
	case it.comp > 0:
		acts = x.por.outside(int(it.comp) - 1)
	case x.por != nil:
		if c, ok := x.por.ample(it.st, run); ok {
			ex.comp = int16(c + 1)
			acts = x.por.comps[c]
		}
	}
	if acts == nil {
		acts = x.allActs
	}
	for _, a := range acts {
		if s := run(a); len(s) > 0 {
			if ex.succs == nil {
				ex.succs = make([]succ, 0, len(s)*len(acts))
			}
			ex.succs = append(ex.succs, s...)
		}
	}
	if x.o.PORValidate && it.comp == 0 && ex.comp == 0 {
		if f := w.validatePOR(it.st, per); f != nil {
			ex.fail = f
		}
	}
	for a := range per {
		clear(per[a])
	}
	return ex
}

func (w *worker) runAction(p State, a int, det bool, praw stateKey, out []succ) []succ {
	act := &w.x.m.Actions[a]
	w.where = "action " + act.Name
	if act.Enabled != nil && !act.Enabled(p) {
		return out
	}
	var prefix []int32
	for {
		w.c.reset(prefix, false)
		s := act.Step(&w.c, p.Clone())
		var log []int32
		if len(w.c.vals) > 0 {
			log = slices.Clone(w.c.vals)
		}
		nxt := w.c.next()
		if det {
			var raw1 stateKey
			if s != nil {
				raw1 = w.k.raw(s)
			}
			w.c.reset(log, true)
			s2 := act.Step(&w.c, p.Clone())
			same := (s == nil) == (s2 == nil) && len(w.c.vals) == len(log)
			if same && s != nil {
				same = w.k.raw(s2) == raw1
			}
			if !same && w.k.raw(p) != praw {
				panic(failure{KindDeterminism, act.Name, fmt.Sprintf("action %s modified the state it was given; Clone is not a deep copy", act.Name)})
			}
			if !same {
				panic(failure{KindDeterminism, act.Name, fmt.Sprintf("action %s with choices %v gave different results on two runs from the same state (map iteration, clock, randomness or hidden global?)", act.Name, toInts(log))})
			}
		}
		if s != nil {
			sc := succ{st: s, key: w.k.canonical(s), act: int16(a), choices: log}
			if f := w.eval(&sc); f != nil {
				panic(*f)
			}
			out = append(out, sc)
		}
		if nxt == nil {
			if det && w.k.raw(p) != praw {
				panic(failure{KindDeterminism, act.Name, fmt.Sprintf("action %s modified the state it was given; Clone is not a deep copy", act.Name)})
			}
			return out
		}
		prefix = nxt
		w.where = "action " + act.Name
	}
}

func (w *worker) eval(s *succ) *failure {
	m := w.x.m
	for i, inv := range m.Invariants {
		w.where = "invariant " + inv.Name
		if !inv.Holds(s.st) {
			s.bad = append(s.bad, i)
		}
	}
	for i, ev := range m.Eventually {
		w.where = "eventually " + ev.Name
		if ev.Holds(s.st) {
			s.props |= 1 << i
		}
	}
	for i, r := range m.Responses {
		w.where = "response " + r.Name
		if r.Trigger(s.st) {
			s.resp |= 1 << (2 * i)
		}
		if r.Response(s.st) {
			s.resp |= 1 << (2*i + 1)
		}
	}
	return nil
}

func (w *worker) evalSafe(s *succ) (f *failure) {
	defer func() {
		if r := recover(); r != nil {
			f = w.failFrom(r)
		}
	}()
	return w.eval(s)
}

func (w *worker) failFrom(r any) *failure {
	switch v := r.(type) {
	case failure:
		return &v
	case choiceMismatch:
		return &failure{KindDeterminism, strings.TrimPrefix(w.where, "action "), w.where + ": " + v.msg}
	}
	lines := strings.Split(string(debug.Stack()), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "panic(") {
			lines = lines[min(i+2, len(lines)):]
			break
		}
	}
	stack := strings.Join(lines[:min(len(lines), 8)], "\n")
	return &failure{KindPanic, w.where, fmt.Sprintf("%v\n%s", r, stack)}
}

func toInts(xs []int32) []int {
	out := make([]int, len(xs))
	for i, v := range xs {
		out[i] = int(v)
	}
	return out
}

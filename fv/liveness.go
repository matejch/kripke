package fv

import "slices"

type graph struct {
	off []int32
	to  []int32
	act []int16
}

type hop struct {
	to  int32
	act int16
}

func (x *explorer) buildGraph() *graph {
	n := len(x.nodes)
	slices.SortFunc(x.edges, func(a, b edge) int {
		if a.from != b.from {
			return int(a.from - b.from)
		}
		if a.to != b.to {
			return int(a.to - b.to)
		}
		return int(a.act - b.act)
	})
	x.edges = slices.Compact(x.edges)
	g := &graph{off: make([]int32, n+1), to: make([]int32, len(x.edges)), act: make([]int16, len(x.edges))}
	for i, e := range x.edges {
		g.off[e.from+1]++
		g.to[i] = e.to
		g.act[i] = e.act
	}
	for i := range n {
		g.off[i+1] += g.off[i]
	}
	x.edges = nil
	return g
}

func (g *graph) reverse() *graph {
	n := len(g.off) - 1
	r := &graph{off: make([]int32, n+1), to: make([]int32, len(g.to)), act: make([]int16, len(g.to))}
	for _, v := range g.to {
		r.off[v+1]++
	}
	for i := range n {
		r.off[i+1] += r.off[i]
	}
	fill := slices.Clone(r.off[:n])
	for u := range n {
		for e := g.off[u]; e < g.off[u+1]; e++ {
			v := g.to[e]
			r.to[fill[v]] = int32(u)
			r.act[fill[v]] = g.act[e]
			fill[v]++
		}
	}
	return r
}

func (x *explorer) fairMasks() (wf, sf uint64) {
	for i, a := range x.m.Actions {
		if a.Fair {
			wf |= 1 << i
		}
		if a.StrongFair {
			sf |= 1 << i
		}
	}
	return wf, sf
}

func (x *explorer) fairComps(g *graph, allowed func(int32) bool) [][]int32 {
	n := int32(len(x.nodes))
	region := make([]int32, n)
	var start []int32
	for u := range n {
		if allowed == nil || allowed(u) {
			start = append(start, u)
		} else {
			region[u] = -1
		}
	}
	type job struct {
		id      int32
		members []int32
	}
	queue := []job{{0, start}}
	nextRegion := int32(1)
	wf, sf := x.fairMasks()
	t := newTarjan(int(n))
	var accepted [][]int32
	for qi := 0; qi < len(queue); qi++ {
		j := queue[qi]
		for _, c := range t.run(g, j.members, j.id, region) {
			if !t.cyclic(g, c, j.id, region) {
				continue
			}
			all, anyEn := ^uint64(0), uint64(0)
			var taken uint64
			for _, u := range c {
				all &= x.nodes[u].enabled
				anyEn |= x.nodes[u].enabled
				for e := g.off[u]; e < g.off[u+1]; e++ {
					v := g.to[e]
					if region[v] == j.id && t.scc[v] == t.scc[u] {
						taken |= 1 << g.act[e]
					}
				}
			}
			if (all&wf)&^taken != 0 {
				continue
			}
			if bad := (anyEn & sf) &^ taken; bad != 0 {
				id := nextRegion
				nextRegion++
				var keep []int32
				for _, u := range c {
					if x.nodes[u].enabled&bad == 0 {
						region[u] = id
						keep = append(keep, u)
					} else {
						region[u] = -1
					}
				}
				if len(keep) > 0 {
					queue = append(queue, job{id, keep})
				}
				continue
			}
			for _, u := range c {
				region[u] = -2
			}
			accepted = append(accepted, c)
		}
	}
	return accepted
}

type tarjan struct {
	index, low, scc []int32
	onStack         []bool
	nextSCC         int32
}

func newTarjan(n int) *tarjan {
	return &tarjan{index: make([]int32, n), low: make([]int32, n), scc: make([]int32, n), onStack: make([]bool, n)}
}

func (t *tarjan) run(g *graph, members []int32, rid int32, region []int32) [][]int32 {
	for _, u := range members {
		t.index[u] = -1
	}
	type frame struct{ u, e int32 }
	var out [][]int32
	var stack []int32
	var frames []frame
	counter := int32(0)
	for _, root := range members {
		if t.index[root] != -1 {
			continue
		}
		t.index[root], t.low[root] = counter, counter
		counter++
		stack = append(stack, root)
		t.onStack[root] = true
		frames = append(frames, frame{root, g.off[root]})
		for len(frames) > 0 {
			f := &frames[len(frames)-1]
			u := f.u
			if f.e < g.off[u+1] {
				v := g.to[f.e]
				f.e++
				if region[v] != rid {
					continue
				}
				if t.index[v] == -1 {
					t.index[v], t.low[v] = counter, counter
					counter++
					stack = append(stack, v)
					t.onStack[v] = true
					frames = append(frames, frame{v, g.off[v]})
				} else if t.onStack[v] {
					t.low[u] = min(t.low[u], t.index[v])
				}
				continue
			}
			if t.low[u] == t.index[u] {
				var c []int32
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					t.onStack[w] = false
					t.scc[w] = t.nextSCC
					c = append(c, w)
					if w == u {
						break
					}
				}
				t.nextSCC++
				slices.Sort(c)
				out = append(out, c)
			}
			frames = frames[:len(frames)-1]
			if len(frames) > 0 {
				p := frames[len(frames)-1].u
				t.low[p] = min(t.low[p], t.low[u])
			}
		}
	}
	return out
}

func (t *tarjan) cyclic(g *graph, c []int32, rid int32, region []int32) bool {
	if len(c) > 1 {
		return true
	}
	u := c[0]
	for e := g.off[u]; e < g.off[u+1]; e++ {
		if g.to[e] == u && region[u] == rid {
			return true
		}
	}
	return false
}

func (x *explorer) liveness() {
	if !x.keepEdges {
		return
	}
	g := x.buildGraph()
	w := x.newWorker()
	if len(x.m.Eventually) > 0 {
		comps := x.fairComps(g, nil)
		for i, ev := range x.m.Eventually {
			if x.reported[KindEventually+"\x00"+ev.Name] {
				continue
			}
			best, bestComp := int32(-1), -1
			for ci, c := range comps {
				for _, u := range c {
					if x.nodes[u].props&(1<<i) == 0 && (best < 0 || u < best) {
						best, bestComp = u, ci
					}
				}
			}
			if best < 0 {
				continue
			}
			tr := x.pathTo(best)
			x.appendLoop(w, g, &tr, best, comps[bestComp])
			x.report(KindLiveness, ev.Name, "fair cycle on which it does not hold forever", tr)
			if x.stopped {
				return
			}
		}
	}
	var rev *graph
	for i, r := range x.m.Responses {
		tb, rb := uint64(1)<<(2*i), uint64(1)<<(2*i+1)
		allowed := func(u int32) bool { return x.nodes[u].resp&rb == 0 }
		comps := x.fairComps(g, allowed)
		compOf := map[int32]int{}
		var frontier []int32
		bad := make([]bool, len(x.nodes))
		for ci, c := range comps {
			for _, u := range c {
				compOf[u] = ci
				bad[u] = true
				frontier = append(frontier, u)
			}
		}
		for u := range int32(len(x.nodes)) {
			if x.nodes[u].quiescent && allowed(u) && !bad[u] {
				bad[u] = true
				frontier = append(frontier, u)
			}
		}
		if len(frontier) == 0 {
			continue
		}
		if rev == nil {
			rev = g.reverse()
		}
		reach := slices.Clone(bad)
		for len(frontier) > 0 {
			v := frontier[len(frontier)-1]
			frontier = frontier[:len(frontier)-1]
			for e := rev.off[v]; e < rev.off[v+1]; e++ {
				u := rev.to[e]
				if !reach[u] && allowed(u) {
					reach[u] = true
					frontier = append(frontier, u)
				}
			}
		}
		trig := int32(-1)
		for u := range int32(len(x.nodes)) {
			if reach[u] && x.nodes[u].resp&tb != 0 {
				trig = u
				break
			}
		}
		if trig < 0 {
			continue
		}
		tr := x.pathTo(trig)
		hops, end := bfsPath(g, trig, func(v int32) bool { return allowed(v) }, func(v int32) bool { return bad[v] }, false)
		x.replayHops(w, &tr, hops)
		if ci, ok := compOf[end]; ok {
			x.appendLoop(w, g, &tr, end, comps[ci])
		}
		x.report(KindResponse, r.Name, "triggered, then a fair execution never reaches the response", tr)
		if x.stopped {
			return
		}
	}
}

func bfsPath(g *graph, from int32, inside, goal func(int32) bool, atLeastOne bool) ([]hop, int32) {
	if !atLeastOne && goal(from) {
		return nil, from
	}
	prev := map[int32]hop{}
	parent := map[int32]int32{}
	queue := []int32{from}
	seen := map[int32]bool{}
	if !atLeastOne {
		seen[from] = true
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for e := g.off[u]; e < g.off[u+1]; e++ {
			v := g.to[e]
			if seen[v] || !inside(v) {
				continue
			}
			seen[v] = true
			prev[v] = hop{v, g.act[e]}
			parent[v] = u
			if goal(v) {
				var path []hop
				for c := v; ; {
					path = append(path, prev[c])
					p := parent[c]
					if p == from {
						break
					}
					c = p
				}
				slices.Reverse(path)
				return path, v
			}
			queue = append(queue, v)
		}
	}
	return nil, -1
}

func (x *explorer) replayHops(w *worker, tr *Trace, hops []hop) {
	for _, h := range hops {
		st, step := x.stepTo(w, tr.last, h.act, x.nodes[h.to].key)
		tr.Steps = append(tr.Steps, step)
		tr.last = st
	}
}

func (x *explorer) appendLoop(w *worker, g *graph, tr *Trace, s int32, comp []int32) {
	in := make(map[int32]bool, len(comp))
	for _, u := range comp {
		in[u] = true
	}
	inside := func(v int32) bool { return in[v] }
	tr.LoopsTo = len(tr.Steps) - 1
	startRaw := w.k.raw(tr.last)
	wf, sf := x.fairMasks()
	all, anyEn := ^uint64(0), uint64(0)
	for _, u := range comp {
		all &= x.nodes[u].enabled
		anyEn |= x.nodes[u].enabled
	}
	needEdge := (all & wf) | (anyEn & sf)
	needOff := wf &^ all
	cur := s
	var loop []hop
	visited := map[int32]bool{s: true}
	var taken uint64
	for a := range len(x.m.Actions) {
		bit := uint64(1) << a
		switch {
		case needEdge&bit != 0 && taken&bit == 0:
			path, end := bfsPath(g, cur, inside, func(v int32) bool { return x.hasEdge(g, v, int16(a), in) }, false)
			for _, h := range path {
				visited[h.to] = true
				taken |= 1 << h.act
			}
			loop = append(loop, path...)
			e := x.edgeWith(g, end, int16(a), in)
			loop = append(loop, e)
			taken |= bit
			visited[e.to] = true
			cur = e.to
		case needOff&bit != 0:
			done := false
			for v := range visited {
				if x.nodes[v].enabled&bit == 0 {
					done = true
				}
			}
			if done {
				continue
			}
			path, end := bfsPath(g, cur, inside, func(v int32) bool { return x.nodes[v].enabled&bit == 0 }, false)
			for _, h := range path {
				visited[h.to] = true
				taken |= 1 << h.act
			}
			loop = append(loop, path...)
			cur = end
		}
	}
	back, _ := bfsPath(g, cur, inside, func(v int32) bool { return v == s }, len(loop) == 0)
	loop = append(loop, back...)
	x.replayHops(w, tr, loop)
	if w.k.raw(tr.last) != startRaw {
		tr.UpToSym = true
	}
}

func (x *explorer) hasEdge(g *graph, u int32, a int16, in map[int32]bool) bool {
	for e := g.off[u]; e < g.off[u+1]; e++ {
		if g.act[e] == a && in[g.to[e]] {
			return true
		}
	}
	return false
}

func (x *explorer) edgeWith(g *graph, u int32, a int16, in map[int32]bool) hop {
	for e := g.off[u]; e < g.off[u+1]; e++ {
		if g.act[e] == a && in[g.to[e]] {
			return hop{g.to[e], a}
		}
	}
	panic("fv: internal error: missing fair edge")
}

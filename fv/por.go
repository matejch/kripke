package fv

import (
	"fmt"
	"slices"
)

type porInfo struct {
	indep   [][]bool
	comps   [][]int
	visible []bool
	all     int
}

func fieldSet(list []string) map[string]bool {
	if list == nil {
		return nil
	}
	m := make(map[string]bool, len(list))
	for _, f := range list {
		m[f] = true
	}
	return m
}

func overlaps(a, b map[string]bool) bool {
	if a == nil || b == nil {
		return true
	}
	for f := range a {
		if b[f] {
			return true
		}
	}
	return false
}

func independence(m *Model) [][]bool {
	n := len(m.Actions)
	reads := make([]map[string]bool, n)
	writes := make([]map[string]bool, n)
	for i, a := range m.Actions {
		reads[i], writes[i] = fieldSet(a.Reads), fieldSet(a.Writes)
	}
	indep := make([][]bool, n)
	for i := range n {
		indep[i] = make([]bool, n)
		for j := range n {
			if i == j {
				continue
			}
			indep[i][j] = !overlaps(writes[i], reads[j]) && !overlaps(writes[i], writes[j]) && !overlaps(writes[j], reads[i])
		}
	}
	return indep
}

func newPOR(m *Model) *porInfo {
	n := len(m.Actions)
	p := &porInfo{indep: independence(m), visible: make([]bool, n), all: n}
	for i, a := range m.Actions {
		w := fieldSet(a.Writes)
		for _, inv := range m.Invariants {
			if overlaps(w, fieldSet(inv.Reads)) {
				p.visible[i] = true
			}
		}
	}
	comp := make([]int, n)
	for i := range comp {
		comp[i] = -1
	}
	for i := range n {
		if comp[i] >= 0 {
			continue
		}
		id := len(p.comps)
		group := []int{i}
		comp[i] = id
		for k := 0; k < len(group); k++ {
			for j := range n {
				if comp[j] < 0 && !p.indep[group[k]][j] {
					comp[j] = id
					group = append(group, j)
				}
			}
		}
		slices.Sort(group)
		p.comps = append(p.comps, group)
	}
	slices.SortStableFunc(p.comps, func(a, b []int) int { return len(a) - len(b) })
	return p
}

func (p *porInfo) ample(s State, run func(int) []succ) (int, bool) {
	if len(p.comps) < 2 {
		return 0, false
	}
outer:
	for c, group := range p.comps {
		for _, a := range group {
			if p.visible[a] {
				continue outer
			}
		}
		for _, a := range group {
			if len(run(a)) == 0 {
				continue outer
			}
		}
		return c, true
	}
	return 0, false
}

func (p *porInfo) outside(c int) []int {
	var out []int
	for a := range p.all {
		if !slices.Contains(p.comps[c], a) {
			out = append(out, a)
		}
	}
	return out
}

func (w *worker) validatePOR(s State, per [][]succ) *failure {
	m := w.x.m
	indep := independence(m)
	keysAfter := func(from []succ, b int) ([]stateKey, bool) {
		var out []stateKey
		for _, sa := range from {
			next := w.runAction(sa.st, b, false, stateKey{}, nil)
			if len(next) == 0 {
				return nil, false
			}
			for _, sb := range next {
				out = append(out, sb.key)
			}
		}
		slices.SortFunc(out, func(x, y stateKey) int { return slices.Compare(x[:], y[:]) })
		return out, true
	}
	for a := range m.Actions {
		for b := a + 1; b < len(m.Actions); b++ {
			if !indep[a][b] || len(per[a]) == 0 || len(per[b]) == 0 {
				continue
			}
			ab, okA := keysAfter(per[a], b)
			ba, okB := keysAfter(per[b], a)
			name := m.Actions[a].Name + "|" + m.Actions[b].Name
			switch {
			case !okA:
				return &failure{KindPOR, name, fmt.Sprintf("declared independent, but %s disables %s", m.Actions[a].Name, m.Actions[b].Name)}
			case !okB:
				return &failure{KindPOR, name, fmt.Sprintf("declared independent, but %s disables %s", m.Actions[b].Name, m.Actions[a].Name)}
			case !slices.Equal(ab, ba):
				return &failure{KindPOR, name, "declared independent, but the two orders reach different states; fix Reads/Writes"}
			}
		}
	}
	return nil
}

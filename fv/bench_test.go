package fv

import (
	"slices"
	"testing"
)

type grid struct{ C []int8 }

func (g *grid) Clone() State { return &grid{C: slices.Clone(g.C)} }

func gridModel(k, limit int) Model {
	m := Model{Name: "grid", Init: func() []State { return []State{&grid{C: make([]int8, k)}} }}
	for i := range k {
		m.Actions = append(m.Actions, Action{Name: string(rune('a' + i)), Step: func(c *Choice, s State) State {
			g := s.(*grid)
			if int(g.C[i]) >= limit {
				return nil
			}
			g.C[i]++
			return g
		}})
	}
	return m
}

func BenchmarkGrid(b *testing.B) {
	for _, w := range []int{1, 0} {
		name := "serial"
		if w == 0 {
			name = "parallel"
		}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				r := Run(gridModel(6, 9), Options{Workers: w})
				b.ReportMetric(float64(r.States)/r.Elapsed.Seconds(), "states/s")
			}
		})
	}
}

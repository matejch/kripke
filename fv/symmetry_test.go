package fv

import (
	"slices"
	"strings"
	"testing"
)

type Pid int

type Name string

type proc struct {
	ID   Pid
	Done bool
}

type procs struct {
	Ps []proc `fv:"set"`
}

func (s *procs) Clone() State { return &procs{Ps: slices.Clone(s.Ps)} }

func procModel(k int) Model {
	return Model{
		Name: "procs",
		Init: func() []State {
			s := &procs{}
			for i := range k {
				s.Ps = append(s.Ps, proc{ID: Pid(i)})
			}
			return []State{s}
		},
		Actions: []Action{{Name: "finish", Step: func(c *Choice, s State) State {
			p := &s.(*procs).Ps[c.Pick(len(s.(*procs).Ps))]
			if p.Done {
				return nil
			}
			p.Done = true
			return s
		}}},
		Symmetries: []Symmetry{Sym[Pid](0, 1, 2, 3)},
	}
}

func TestSymmetryCollapsesRenamings(t *testing.T) {
	sym, full := Run(procModel(4), Options{}), Run(procModel(4), Options{NoSymmetry: true})
	if full.States != 16 || sym.States != 5 {
		t.Fatalf("want 16 vs 5, got %d vs %d", full.States, sym.States)
	}
}

func TestSymmetryNeedsOrderFreeContainers(t *testing.T) {
	m := procModel(4)
	m.Init = func() []State {
		return []State{&plainProcs{Ps: []proc{{0, false}, {1, false}, {2, false}, {3, false}}}}
	}
	m.Actions[0].Step = func(c *Choice, s State) State {
		p := &s.(*plainProcs).Ps[c.Pick(4)]
		if p.Done {
			return nil
		}
		p.Done = true
		return s
	}
	if r := Run(m, Options{}); r.States != 16 {
		t.Fatalf("a positional slice keeps identities apart, want 16 got %s", RenderResult(r))
	}
}

type plainProcs struct{ Ps []proc }

func (s *plainProcs) Clone() State { return &plainProcs{Ps: slices.Clone(s.Ps)} }

func TestSymmetryRequiresANamedType(t *testing.T) {
	m := procModel(2)
	m.Symmetries = []Symmetry{Sym[int](0, 1)}
	if r := Run(m, Options{}); !strings.Contains(r.Error, "named") {
		t.Fatalf("got %q", r.Error)
	}
}

type keyedProcs struct{ procs }

func (k *keyedProcs) Key() []byte  { return Key(&k.procs) }
func (k *keyedProcs) Clone() State { return &keyedProcs{*k.procs.Clone().(*procs)} }

func TestKeyerAndSymmetryConflict(t *testing.T) {
	m := procModel(2)
	m.Init = func() []State { return []State{&keyedProcs{procs{Ps: []proc{{ID: 0}, {ID: 1}}}}} }
	if r := Run(m, Options{}); !strings.Contains(r.Error, "Keyer") {
		t.Fatalf("got %q", r.Error)
	}
}

type named struct {
	Who  []Name `fv:"set"`
	Last Name
}

func (s *named) Clone() State { return &named{Who: slices.Clone(s.Who), Last: s.Last} }

func TestStringSymmetry(t *testing.T) {
	m := Model{
		Name: "names",
		Init: func() []State { return []State{&named{}} },
		Actions: []Action{{Name: "add", Step: func(c *Choice, s State) State {
			st := s.(*named)
			x := Any(c, []Name{"ann", "bob", "cid"})
			if slices.Contains(st.Who, x) {
				return nil
			}
			st.Who, st.Last = append(st.Who, x), x
			return st
		}}},
		Symmetries: []Symmetry{Sym[Name]("ann", "bob", "cid")},
	}
	if r := Run(m, Options{}); r.States != 4 {
		t.Fatalf("want 4 (0..3 names), got %s", RenderResult(r))
	}
}

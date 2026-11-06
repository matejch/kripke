package fv

import (
	"bytes"
	"testing"
)

type keyed struct {
	Q   []int
	M   map[string]int
	Set []int `fv:"set"`
	Cfg int   `fv:"-"`
	P   *int
	Any any
}

func (k *keyed) Clone() State { c := *k; return &c }

func TestNilAndEmptyEncodeTheSame(t *testing.T) {
	if !bytes.Equal(Key(&keyed{}), Key(&keyed{Q: []int{}, M: map[string]int{}})) {
		t.Fatal("nil and empty should match")
	}
}

func TestMapsEncodeOrderFree(t *testing.T) {
	a, b := &keyed{M: map[string]int{}}, &keyed{M: map[string]int{}}
	for i := range 50 {
		a.M[string(rune('a'+i))] = i
	}
	for i := 49; i >= 0; i-- {
		b.M[string(rune('a'+i))] = i
	}
	if !bytes.Equal(Key(a), Key(b)) {
		t.Fatal("map order leaked into the key")
	}
}

func TestSetTagAndConfigSkip(t *testing.T) {
	if !bytes.Equal(Key(&keyed{Set: []int{3, 1, 2}, Cfg: 1}), Key(&keyed{Set: []int{1, 2, 3}, Cfg: 2})) {
		t.Fatal("set order or fv:\"-\" field leaked into the key")
	}
	if bytes.Equal(Key(&keyed{Q: []int{1, 2}}), Key(&keyed{Q: []int{2, 1}})) {
		t.Fatal("plain slices are ordered")
	}
}

func TestPointersAndInterfaces(t *testing.T) {
	one, other := 1, 1
	if !bytes.Equal(Key(&keyed{P: &one}), Key(&keyed{P: &other})) {
		t.Fatal("pointers should be followed, not compared")
	}
	if bytes.Equal(Key(&keyed{Any: int(1)}), Key(&keyed{Any: int64(1)})) {
		t.Fatal("interface keys must include the dynamic type")
	}
}

type withChan struct{ C chan int }

func (w *withChan) Clone() State { return w }

func TestChansAreRejected(t *testing.T) {
	m := Model{Name: "chan", Init: func() []State { return []State{&withChan{}} }, Actions: []Action{{Name: "x", Step: func(*Choice, State) State { return nil }}}}
	if r := Run(m, Options{}); r.Error == "" {
		t.Fatal("want an error for a chan field")
	}
	if a, b := Run(counter(50), Options{}), Run(counter(50), Options{SHA256: true}); a.States != b.States {
		t.Fatal("sha256 changed the state count")
	}
}

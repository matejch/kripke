package fv

import (
	"fmt"
	"reflect"
	"time"
)

// State is a model state; Clone must return a deep copy because the engine never mutates in place.
type State interface{ Clone() State }

// Keyer overrides the reflection encoding of a state's identity; the model must canonicalize itself.
type Keyer interface{ Key() []byte }

// Model is a system to check: initial states, actions and properties.
type Model struct {
	Name            string
	Init            func() []State
	Actions         []Action
	Invariants      []Invariant
	Eventually      []Invariant
	DeadlockIsError bool
	Symmetries      []Symmetry
	Responses       []Response
}

// Action is one kind of transition; Step gets a clone and returns nil when the branch does not apply.
type Action struct {
	Name       string
	Enabled    func(s State) bool
	Step       func(c *Choice, s State) State
	Fair       bool
	StrongFair bool
	Reads      []string
	Writes     []string
}

// Invariant is a named state predicate; Reads lists the top-level fields it reads, nil meaning all.
type Invariant struct {
	Name  string
	Holds func(State) bool
	Reads []string
}

// Symmetry declares interchangeable values of one named integer or string type.
type Symmetry struct {
	typ  reflect.Type
	ints []int64
	strs []string
}

// Response is the property "whenever Trigger holds, Response eventually holds".
type Response struct {
	Name     string
	Trigger  func(State) bool
	Response func(State) bool
}

// Options bound and tune a run; the zero value is an unbounded, parallel, determinism-checked BFS.
type Options struct {
	MaxDepth               int
	MaxStates              int
	Deadline               time.Duration
	DFS                    bool
	NoDeterminismCheck     bool
	ContinueAfterViolation bool
	Workers                int
	NoSymmetry             bool
	POR                    bool
	PORValidate            bool
	SHA256                 bool
	Simulate               int
	Seed                   uint64
	Progress               func(Progress)
}

// Progress is reported once per BFS layer when Options.Progress is set.
type Progress struct {
	Depth, States, Frontier int
	Elapsed                 time.Duration
}

const (
	maxActions    = 64
	maxEventually = 64
	maxResponses  = 32
	maxChoiceLen  = 1<<15 - 1
	maxInits      = 1<<15 - 1
	maxNodes      = 1<<31 - 2
)

// Inv builds an Invariant; reads optionally lists the top-level fields it reads.
func Inv(name string, holds func(State) bool, reads ...string) Invariant {
	return Invariant{Name: name, Holds: holds, Reads: reads}
}

// Resp builds a Response property.
func Resp(name string, trigger, response func(State) bool) Response {
	return Response{Name: name, Trigger: trigger, Response: response}
}

// Sym declares the given values of named type T interchangeable; keep the count at 4 or below.
func Sym[T comparable](vals ...T) Symmetry {
	var zero T
	t := reflect.TypeOf(zero)
	s := Symmetry{typ: t}
	for _, v := range vals {
		rv := reflect.ValueOf(v)
		switch {
		case isInt(t.Kind()):
			s.ints = append(s.ints, rv.Int())
		case isUint(t.Kind()):
			s.ints = append(s.ints, int64(rv.Uint()))
		case t.Kind() == reflect.String:
			s.strs = append(s.strs, rv.String())
		}
	}
	return s
}

func isInt(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Int64
}

func isUint(k reflect.Kind) bool {
	return k >= reflect.Uint && k <= reflect.Uintptr
}

func (s Symmetry) size() int { return len(s.ints) + len(s.strs) }

func (m *Model) validate(o Options, sample State) error {
	if m.Name == "" {
		return fmt.Errorf("model has no name")
	}
	if m.Init == nil {
		return fmt.Errorf("model %s: Init is nil", m.Name)
	}
	if len(m.Actions) == 0 {
		return fmt.Errorf("model %s: no actions", m.Name)
	}
	if len(m.Actions) > maxActions {
		return fmt.Errorf("model %s: %d actions, the limit is %d", m.Name, len(m.Actions), maxActions)
	}
	if len(m.Eventually) > maxEventually {
		return fmt.Errorf("model %s: %d Eventually properties, the limit is %d", m.Name, len(m.Eventually), maxEventually)
	}
	if len(m.Responses) > maxResponses {
		return fmt.Errorf("model %s: %d responses, the limit is %d", m.Name, len(m.Responses), maxResponses)
	}
	names := map[string]bool{}
	for _, a := range m.Actions {
		if a.Name == "" || a.Step == nil {
			return fmt.Errorf("model %s: every action needs a Name and a Step", m.Name)
		}
		if names["a:"+a.Name] {
			return fmt.Errorf("model %s: duplicate action %q", m.Name, a.Name)
		}
		names["a:"+a.Name] = true
	}
	for _, group := range [][]Invariant{m.Invariants, m.Eventually} {
		for _, inv := range group {
			if inv.Name == "" || inv.Holds == nil {
				return fmt.Errorf("model %s: every invariant needs a Name and Holds", m.Name)
			}
			if names["i:"+inv.Name] {
				return fmt.Errorf("model %s: duplicate property %q", m.Name, inv.Name)
			}
			names["i:"+inv.Name] = true
		}
	}
	for _, r := range m.Responses {
		if r.Name == "" || r.Trigger == nil || r.Response == nil {
			return fmt.Errorf("model %s: every response needs a Name, Trigger and Response", m.Name)
		}
	}
	if !o.NoSymmetry {
		for _, s := range m.Symmetries {
			if s.typ == nil || s.typ.PkgPath() == "" || !(isInt(s.typ.Kind()) || isUint(s.typ.Kind()) || s.typ.Kind() == reflect.String) {
				return fmt.Errorf("model %s: symmetry needs a named integer or string type", m.Name)
			}
			if s.size() < 2 {
				return fmt.Errorf("model %s: symmetry over %s needs at least 2 values", m.Name, s.typ)
			}
		}
		if _, ok := sample.(Keyer); ok && len(m.Symmetries) > 0 {
			return fmt.Errorf("model %s: state implements Keyer, so it must canonicalize itself; drop Symmetries or Key()", m.Name)
		}
	}
	if o.POR {
		if o.DFS || o.Simulate > 0 {
			return fmt.Errorf("model %s: --por works with BFS only", m.Name)
		}
		if len(m.Eventually) > 0 || len(m.Responses) > 0 {
			return fmt.Errorf("model %s: --por preserves safety only; this model has liveness properties", m.Name)
		}
		if len(m.Symmetries) > 0 && !o.NoSymmetry {
			return fmt.Errorf("model %s: --por with symmetry is not supported; add --no-symmetry", m.Name)
		}
	}
	fields := topFields(sample)
	check := func(owner string, list []string) error {
		for _, f := range list {
			if !fields[f] {
				return fmt.Errorf("model %s: %s names unknown field %q", m.Name, owner, f)
			}
		}
		return nil
	}
	for _, a := range m.Actions {
		if err := check("action "+a.Name, append(append([]string{}, a.Reads...), a.Writes...)); err != nil {
			return err
		}
	}
	for _, inv := range m.Invariants {
		if err := check("invariant "+inv.Name, inv.Reads); err != nil {
			return err
		}
	}
	return nil
}

func topFields(s State) map[string]bool {
	out := map[string]bool{}
	t := reflect.TypeOf(s)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := range t.NumField() {
		out[t.Field(i).Name] = true
	}
	return out
}

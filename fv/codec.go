package fv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sync"
)

type encFn func(e *encoder, v reflect.Value)

type symMap struct {
	ints, toInts []int64
	strs, toStrs []string
}

type encoder struct {
	buf   []byte
	syms  map[reflect.Type]*symMap
	depth int
	plans map[reflect.Type]encFn
}

const maxPointerDepth = 10000

var (
	planMu sync.RWMutex
	plans  = map[reflect.Type]encFn{}
)

func planFor(t reflect.Type) encFn {
	planMu.RLock()
	fn, ok := plans[t]
	planMu.RUnlock()
	if ok {
		return fn
	}
	planMu.Lock()
	defer planMu.Unlock()
	return buildPlan(t)
}

func buildPlan(t reflect.Type) encFn {
	if fn, ok := plans[t]; ok {
		return fn
	}
	var real encFn
	plans[t] = func(e *encoder, v reflect.Value) { real(e, v) }
	defer func() {
		if real == nil {
			delete(plans, t)
		}
	}()
	real = compile(t)
	plans[t] = real
	return real
}

func compile(t reflect.Type) encFn {
	named := t.Name() != "" && t.PkgPath() != ""
	switch k := t.Kind(); {
	case k == reflect.Bool:
		return func(e *encoder, v reflect.Value) {
			if v.Bool() {
				e.buf = append(e.buf, 1)
			} else {
				e.buf = append(e.buf, 0)
			}
		}
	case isInt(k):
		if named {
			return func(e *encoder, v reflect.Value) {
				x := v.Int()
				if e.syms != nil {
					if m := e.syms[t]; m != nil {
						x = m.mapInt(x)
					}
				}
				e.buf = binary.AppendVarint(e.buf, x)
			}
		}
		return func(e *encoder, v reflect.Value) { e.buf = binary.AppendVarint(e.buf, v.Int()) }
	case isUint(k):
		if named {
			return func(e *encoder, v reflect.Value) {
				x := v.Uint()
				if e.syms != nil {
					if m := e.syms[t]; m != nil {
						x = uint64(m.mapInt(int64(x)))
					}
				}
				e.buf = binary.AppendUvarint(e.buf, x)
			}
		}
		return func(e *encoder, v reflect.Value) { e.buf = binary.AppendUvarint(e.buf, v.Uint()) }
	case k == reflect.Float32 || k == reflect.Float64:
		return func(e *encoder, v reflect.Value) {
			e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(v.Float()))
		}
	case k == reflect.Complex64 || k == reflect.Complex128:
		return func(e *encoder, v reflect.Value) {
			c := v.Complex()
			e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(real(c)))
			e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(imag(c)))
		}
	case k == reflect.String:
		return func(e *encoder, v reflect.Value) {
			s := v.String()
			if named && e.syms != nil {
				if m := e.syms[t]; m != nil {
					s = m.mapStr(s)
				}
			}
			e.buf = binary.AppendUvarint(e.buf, uint64(len(s)))
			e.buf = append(e.buf, s...)
		}
	case k == reflect.Array:
		el := buildPlan(t.Elem())
		return func(e *encoder, v reflect.Value) {
			for i := range v.Len() {
				el(e, v.Index(i))
			}
		}
	case k == reflect.Slice:
		el := buildPlan(t.Elem())
		return func(e *encoder, v reflect.Value) {
			e.buf = binary.AppendUvarint(e.buf, uint64(v.Len()))
			for i := range v.Len() {
				el(e, v.Index(i))
			}
		}
	case k == reflect.Map:
		kf, vf := buildPlan(t.Key()), buildPlan(t.Elem())
		return func(e *encoder, v reflect.Value) {
			var parts [][]byte
			it := v.MapRange()
			for it.Next() {
				parts = append(parts, e.sub(func() { kf(e, it.Key()); vf(e, it.Value()) }))
			}
			e.writeSorted(parts)
		}
	case k == reflect.Pointer:
		el := buildPlan(t.Elem())
		return func(e *encoder, v reflect.Value) {
			if v.IsNil() {
				e.buf = append(e.buf, 0)
				return
			}
			e.depth++
			if e.depth > maxPointerDepth {
				panic(fmt.Sprintf("fv: pointers nested deeper than %d at %s; is the state cyclic?", maxPointerDepth, t))
			}
			e.buf = append(e.buf, 1)
			el(e, v.Elem())
			e.depth--
		}
	case k == reflect.Interface:
		return func(e *encoder, v reflect.Value) {
			if v.IsNil() {
				e.buf = append(e.buf, 0)
				return
			}
			d := v.Elem()
			name := d.Type().String()
			e.buf = append(e.buf, 1)
			e.buf = binary.AppendUvarint(e.buf, uint64(len(name)))
			e.buf = append(e.buf, name...)
			e.plan(d.Type())(e, d)
		}
	case k == reflect.Struct:
		type field struct {
			idx int
			fn  encFn
		}
		var fields []field
		for i := range t.NumField() {
			f := t.Field(i)
			switch f.Tag.Get("fv") {
			case "-":
				continue
			case "set":
				if f.Type.Kind() != reflect.Slice && f.Type.Kind() != reflect.Array {
					panic(fmt.Sprintf("fv: field %s.%s: fv:\"set\" needs a slice or array", t, f.Name))
				}
				fields = append(fields, field{i, setPlan(f.Type)})
			default:
				fields = append(fields, field{i, buildPlan(f.Type)})
			}
		}
		return func(e *encoder, v reflect.Value) {
			for _, f := range fields {
				f.fn(e, v.Field(f.idx))
			}
		}
	default:
		panic(fmt.Sprintf("fv: cannot encode %s (%s) in a state; chans, funcs and unsafe pointers are not state", t, k))
	}
}

func setPlan(t reflect.Type) encFn {
	el := buildPlan(t.Elem())
	return func(e *encoder, v reflect.Value) {
		var parts [][]byte
		for i := range v.Len() {
			parts = append(parts, e.sub(func() { el(e, v.Index(i)) }))
		}
		e.writeSorted(parts)
	}
}

func (e *encoder) sub(f func()) []byte {
	saved := e.buf
	e.buf = nil
	f()
	out := e.buf
	e.buf = saved
	return out
}

func (e *encoder) writeSorted(parts [][]byte) {
	slices.SortFunc(parts, bytes.Compare)
	e.buf = binary.AppendUvarint(e.buf, uint64(len(parts)))
	for _, p := range parts {
		e.buf = binary.AppendUvarint(e.buf, uint64(len(p)))
		e.buf = append(e.buf, p...)
	}
}

func (m *symMap) mapInt(x int64) int64 {
	for i, v := range m.ints {
		if v == x {
			return m.toInts[i]
		}
	}
	return x
}

func (m *symMap) mapStr(x string) string {
	for i, v := range m.strs {
		if v == x {
			return m.toStrs[i]
		}
	}
	return x
}

func (e *encoder) plan(t reflect.Type) encFn {
	if fn, ok := e.plans[t]; ok {
		return fn
	}
	if e.plans == nil {
		e.plans = map[reflect.Type]encFn{}
	}
	fn := planFor(t)
	e.plans[t] = fn
	return fn
}

func encodeValue(e *encoder, s State) []byte {
	e.buf = e.buf[:0]
	if k, ok := s.(Keyer); ok {
		e.buf = append(e.buf, k.Key()...)
		return e.buf
	}
	v := reflect.ValueOf(s)
	e.plan(v.Type())(e, v)
	return e.buf
}

func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, p := range permutations(n - 1) {
		for i := 0; i <= len(p); i++ {
			q := make([]int, 0, n)
			q = append(q, p[:i]...)
			q = append(q, n-1)
			q = append(q, p[i:]...)
			out = append(out, q)
		}
	}
	return out
}

func symRenamings(syms []Symmetry) []map[reflect.Type]*symMap {
	out := []map[reflect.Type]*symMap{{}}
	for _, s := range syms {
		var next []map[reflect.Type]*symMap
		for _, p := range permutations(s.size()) {
			m := &symMap{}
			if len(s.ints) > 0 {
				m.ints = s.ints
				m.toInts = make([]int64, len(p))
				for i, j := range p {
					m.toInts[i] = s.ints[j]
				}
			} else {
				m.strs = s.strs
				m.toStrs = make([]string, len(p))
				for i, j := range p {
					m.toStrs[i] = s.strs[j]
				}
			}
			for _, base := range out {
				c := make(map[reflect.Type]*symMap, len(base)+1)
				for k, v := range base {
					c[k] = v
				}
				c[s.typ] = m
				next = append(next, c)
			}
		}
		out = next
	}
	return out
}

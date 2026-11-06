package fv

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

func (x *explorer) pathTo(id int32) Trace {
	var chain []int32
	for n := id; n >= 0; n = x.nodes[n].parent {
		chain = append(chain, n)
	}
	w := x.newWorker()
	tr := Trace{LoopsTo: -1}
	var st State
	for i := len(chain) - 1; i >= 0; i-- {
		n := x.nodes[chain[i]]
		if n.parent < 0 {
			inits := x.m.Init()
			st = inits[n.action]
			tr.Steps = append(tr.Steps, Step{Action: "init", State: flatten(st)})
		} else {
			act := &x.m.Actions[n.action]
			log := x.arena[n.cOff : n.cOff+int32(n.cLen)]
			w.c.reset(log, true)
			st = act.Step(&w.c, st.Clone())
			if st == nil {
				panic(fmt.Sprintf("fv: replay of %s returned nil; the model is not deterministic", act.Name))
			}
			tr.Steps = append(tr.Steps, Step{Action: act.Name, Choices: toInts(log), State: flatten(st)})
		}
		if w.k.canonical(st) != n.key {
			panic(fmt.Sprintf("fv: replay reached a different state at step %d; the model is not deterministic", len(tr.Steps)-1))
		}
	}
	tr.last = st
	return tr
}

func (x *explorer) stepTo(w *worker, from State, act int16, target stateKey) (State, Step) {
	acts := []int{int(act)}
	if act < 0 {
		acts = acts[:0]
		for i := range x.m.Actions {
			acts = append(acts, i)
		}
	}
	for _, a := range acts {
		for _, s := range w.runAction(from, a, false, stateKey{}, nil) {
			if s.key == target {
				return s.st, Step{Action: x.m.Actions[a].Name, Choices: toInts(s.choices), State: flatten(s.st)}
			}
		}
	}
	panic("fv: could not replay a stored edge; the model is not deterministic")
}

// Describe renders a state as one "path = value" line per leaf.
func Describe(s State) string {
	var b strings.Builder
	for _, f := range flatten(s) {
		fmt.Fprintf(&b, "%s = %s\n", f.Path, f.Value)
	}
	return b.String()
}

func flatten(s State) []Field {
	var out []Field
	v := reflect.ValueOf(s)
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	describeInto(&out, "", v)
	return out
}

var stringer = reflect.TypeOf((*fmt.Stringer)(nil)).Elem()

func describeInto(out *[]Field, path string, v reflect.Value) {
	leaf := func(val string) { *out = append(*out, Field{Path: strings.TrimPrefix(path, "."), Value: val}) }
	if v.Type().Implements(stringer) && v.CanInterface() && v.Kind() != reflect.Pointer && v.Kind() != reflect.Interface {
		leaf(v.Interface().(fmt.Stringer).String())
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		n := 0
		for i := range t.NumField() {
			if t.Field(i).Tag.Get("fv") == "-" {
				continue
			}
			n++
			describeInto(out, path+"."+t.Field(i).Name, v.Field(i))
		}
		if n == 0 {
			leaf("{}")
		}
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			leaf("[]")
		}
		for i := range v.Len() {
			describeInto(out, fmt.Sprintf("%s[%d]", path, i), v.Index(i))
		}
	case reflect.Map:
		if v.Len() == 0 {
			leaf("{}")
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return scalar(keys[i]) < scalar(keys[j]) })
		for _, k := range keys {
			describeInto(out, fmt.Sprintf("%s[%s]", path, scalar(k)), v.MapIndex(k))
		}
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			leaf("nil")
			return
		}
		describeInto(out, path, v.Elem())
	default:
		leaf(scalar(v))
	}
}

func scalar(v reflect.Value) string {
	if v.Type().Implements(stringer) && v.CanInterface() {
		return v.Interface().(fmt.Stringer).String()
	}
	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Bool:
		return fmt.Sprint(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprint(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return fmt.Sprint(v.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprint(v.Float())
	}
	if v.CanInterface() {
		return fmt.Sprintf("%v", v.Interface())
	}
	return fmt.Sprintf("<%s>", v.Type())
}

// Render prints a violation as a header plus one state diff per step.
func Render(v Violation) string {
	var b strings.Builder
	steps := len(v.Trace.Steps) - 1
	fmt.Fprintf(&b, "%s %q violated after %d steps\n", v.Kind, v.Name, max(steps, 0))
	if v.Detail != "" {
		for _, l := range strings.Split(strings.TrimRight(v.Detail, "\n"), "\n") {
			fmt.Fprintf(&b, "  | %s\n", l)
		}
	}
	var prev []Field
	for i, s := range v.Trace.Steps {
		label := s.Action
		if len(s.Choices) > 0 {
			label += fmt.Sprintf(" %v", s.Choices)
		}
		mark := ""
		if i == v.Trace.LoopsTo {
			mark = "  <- loop starts"
		}
		fmt.Fprintf(&b, "  %3d  %s%s\n", i, label, mark)
		for _, l := range diff(prev, s.State) {
			fmt.Fprintf(&b, "         %s\n", l)
		}
		prev = s.State
	}
	if v.Trace.LoopsTo >= 0 {
		note := ""
		if v.Trace.UpToSym {
			note = " (up to symmetry)"
		}
		fmt.Fprintf(&b, "  loops back to step %d%s\n", v.Trace.LoopsTo, note)
	}
	return b.String()
}

func diff(prev, cur []Field) []string {
	if prev == nil {
		out := make([]string, len(cur))
		for i, f := range cur {
			out[i] = f.Path + " = " + f.Value
		}
		return out
	}
	old := make(map[string]string, len(prev))
	for _, f := range prev {
		old[f.Path] = f.Value
	}
	seen := make(map[string]bool, len(cur))
	var out []string
	for _, f := range cur {
		seen[f.Path] = true
		o, ok := old[f.Path]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("+ %s = %s", f.Path, f.Value))
		case o != f.Value:
			out = append(out, fmt.Sprintf("%s: %s -> %s", f.Path, o, f.Value))
		}
	}
	for _, f := range prev {
		if !seen[f.Path] && !hasChild(cur, f.Path) {
			out = append(out, fmt.Sprintf("- %s", f.Path))
		}
	}
	return out
}

// RenderResult prints the summary line, warnings and every violation.
func RenderResult(r Result) string {
	var b strings.Builder
	status := "OK"
	switch {
	case r.Error != "":
		status = "ERROR"
	case len(r.Violations) > 0:
		status = "FAIL"
	}
	fmt.Fprintf(&b, "%s %s: ", status, r.Model)
	if r.Simulated > 0 {
		fmt.Fprintf(&b, "%d random walks, %d distinct states, %d transitions, max depth %d, no completeness claim", r.Simulated, r.States, r.Transitions, r.Depth)
	} else {
		fmt.Fprintf(&b, "%d states, %d transitions, depth %d", r.States, r.Transitions, r.Depth)
		if r.Complete {
			b.WriteString(", complete")
		} else {
			fmt.Fprintf(&b, ", INCOMPLETE (%s)", r.Incomplete)
		}
		if r.CyclesChecked {
			b.WriteString(", cycles checked")
		} else {
			b.WriteString(", liveness cycles not checked")
		}
	}
	fmt.Fprintf(&b, ", %s\n", r.Elapsed.Round(1e6))
	if r.Error != "" {
		fmt.Fprintf(&b, "  error: %s\n", r.Error)
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}
	for _, v := range r.Violations {
		b.WriteString(Render(v))
	}
	return b.String()
}

// WriteJSON writes results as a JSON array.
func WriteJSON(w io.Writer, rs ...Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rs)
}

// ReadJSON reads results written by WriteJSON; a single object is accepted too.
func ReadJSON(r io.Reader) ([]Result, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var rs []Result
	if err := json.Unmarshal(data, &rs); err == nil {
		return rs, nil
	}
	var one Result
	if err := json.Unmarshal(data, &one); err != nil {
		return nil, err
	}
	return []Result{one}, nil
}

func hasChild(fs []Field, path string) bool {
	for _, f := range fs {
		if strings.HasPrefix(f.Path, path+"[") || strings.HasPrefix(f.Path, path+".") {
			return true
		}
	}
	return false
}

package fv

import (
	"fmt"
	"reflect"
)

func hygiene(s State) []string {
	if _, ok := s.(Keyer); ok {
		return nil
	}
	var out []string
	seen := map[reflect.Type]bool{}
	var visit func(path string, t reflect.Type)
	visit = func(path string, t reflect.Type) {
		switch t.Kind() {
		case reflect.Map:
			out = append(out, fmt.Sprintf("%s is a map: every state pays a sort per key and an allocation per clone; prefer a sorted slice or an fv:\"set\" slice", path))
			visit(path+"[k]", t.Elem())
		case reflect.Interface:
			out = append(out, fmt.Sprintf("%s is an interface: identity depends on the dynamic type and clones may share data", path))
		case reflect.Pointer, reflect.Slice, reflect.Array:
			visit(path, t.Elem())
		case reflect.Struct:
			if seen[t] {
				return
			}
			seen[t] = true
			for i := range t.NumField() {
				f := t.Field(i)
				if f.Tag.Get("fv") == "-" {
					continue
				}
				visit(path+"."+f.Name, f.Type)
			}
		}
	}
	t := reflect.TypeOf(s)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	visit(t.Name(), t)
	return out
}

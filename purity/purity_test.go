package purity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtures = "./internal/fixtures/"

func check(t *testing.T, name string, p Policy) []Finding {
	t.Helper()
	fs, err := Check(".", fixtures+name, p)
	if err != nil {
		t.Fatalf("Check %s: %v", name, err)
	}
	return fs
}

func byRule(fs []Finding) map[string][]Finding {
	m := map[string][]Finding{}
	for _, f := range fs {
		m[f.Rule] = append(m[f.Rule], f)
	}
	return m
}

func TestCleanPasses(t *testing.T) {
	if fs := check(t, "clean", Policy{}); len(fs) != 0 {
		t.Fatalf("want no findings, got %v", fs)
	}
	AssertPure(t, ".", fixtures+"clean")
}

func TestCleanWithDepPasses(t *testing.T) {
	if fs := check(t, "clean_with_dep", Policy{}); len(fs) != 0 {
		t.Fatalf("want no findings, got %v", fs)
	}
}

func TestDirtyReportsEachRule(t *testing.T) {
	fs := check(t, "dirty", Policy{})
	m := byRule(fs)
	for _, rule := range []string{"import", "go", "chan", "select", "clock", "init", "global"} {
		if len(m[rule]) == 0 {
			t.Errorf("rule %s not reported; findings: %v", rule, fs)
		}
	}
	if len(m["clock"]) != 2 {
		t.Errorf("want time.Now and renamed tm.Since, got %v", m["clock"])
	}
	if got := m["import"]; len(got) != 1 || got[0].Msg != "imports os" {
		t.Errorf("import findings: %v", got)
	}
	if got := m["global"]; len(got) != 1 || !strings.Contains(got[0].Msg, "counter") {
		t.Errorf("global findings: %v", got)
	}
	f := fs[0]
	want := f.File + ":"
	if s := f.String(); !strings.HasPrefix(s, want) || !strings.Contains(s, "["+f.Rule+"] ") {
		t.Errorf("String() = %q", s)
	}
}

func TestDirtyDepTransitiveImport(t *testing.T) {
	fs := check(t, "dirty_dep", Policy{})
	if len(fs) != 1 {
		t.Fatalf("want 1 finding, got %v", fs)
	}
	f := fs[0]
	wantMsg := "imports os via github.com/matejch/kripke/purity/internal/fixtures/dirty_dep/osutil"
	if f.Rule != "import" || f.Msg != wantMsg {
		t.Errorf("got %v, want msg %q", f, wantMsg)
	}
	if f.Pkg != "github.com/matejch/kripke/purity/internal/fixtures/dirty_dep" {
		t.Errorf("Pkg = %q", f.Pkg)
	}
	if filepath.Base(f.File) != "osutil.go" {
		t.Errorf("File = %q", f.File)
	}
}

func TestSegmentPrefix(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"net", true},
		{"net/http", true},
		{"network", false},
		{"os", true},
		{"os/exec", true},
		{"osext", false},
		{"time", false},
		{"github.com/jackc/pgx/v5/pgxpool", true},
		{"github.com/jackc/pgxutil", false},
		{"example.com/network/net", false},
	}
	var p Policy
	for _, c := range cases {
		if got := p.denied(c.path); got != c.want {
			t.Errorf("denied(%q) = %v, want %v", c.path, got, c.want)
		}
	}
	p.DenyImports = []string{"encoding/json"}
	if !p.denied("encoding/json") || p.denied("encoding/jsonx") {
		t.Error("deny_imports not applied on segment boundary")
	}
}

func TestAllowImportsOverride(t *testing.T) {
	if fs := check(t, "dirty_dep", Policy{AllowImports: []string{"os"}}); len(fs) != 0 {
		t.Fatalf("want no findings, got %v", fs)
	}
	p := Policy{AllowImports: []string{"net/http"}}
	if p.denied("net/http") || p.denied("net/http/httptest") || !p.denied("net/rpc") {
		t.Error("allow_imports must override deny on segment prefix only")
	}
}

func TestAllowGlobals(t *testing.T) {
	for _, name := range []string{"counter", "github.com/matejch/kripke/purity/internal/fixtures/dirty.counter"} {
		m := byRule(check(t, "dirty", Policy{AllowGlobals: []string{name}}))
		if len(m["global"]) != 0 {
			t.Errorf("allow_globals %q: still reported %v", name, m["global"])
		}
	}
	m := byRule(check(t, "dirty", Policy{AllowGlobals: []string{"other.counter"}}))
	if len(m["global"]) != 1 {
		t.Errorf("qualified allow for another package must not apply: %v", m["global"])
	}
}

func TestAllowCalls(t *testing.T) {
	m := byRule(check(t, "dirty", Policy{AllowCalls: []string{"time.Since"}}))
	if len(m["clock"]) != 1 || !strings.Contains(m["clock"][0].Msg, "time.Now") {
		t.Errorf("want only time.Now left, got %v", m["clock"])
	}
}

func TestLoadPolicy(t *testing.T) {
	dir := t.TempDir()
	p, err := LoadPolicy(filepath.Join(dir, "missing.yaml"))
	if err != nil || len(p.AllowImports)+len(p.DenyImports)+len(p.AllowGlobals)+len(p.AllowCalls) != 0 {
		t.Fatalf("missing file: %+v, %v", p, err)
	}
	path := filepath.Join(dir, "purity.yaml")
	src := "allow_imports: [sync]\ndeny_imports:\n  - encoding/json\nallow_globals: [table]\nallow_calls: [time.Since]\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if p.AllowImports[0] != "sync" || p.DenyImports[0] != "encoding/json" || p.AllowGlobals[0] != "table" || p.AllowCalls[0] != "time.Since" {
		t.Errorf("parsed %+v", p)
	}
	if err := os.WriteFile(path, []byte("allow_import: [sync]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(path); err == nil {
		t.Error("unknown key must be rejected")
	}
}

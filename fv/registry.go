package fv

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	regMu    sync.Mutex
	registry = map[string]Model{}
)

// Register adds a model to the registry used by Main; duplicate names panic.
func Register(m Model) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, ok := registry[m.Name]; ok {
		panic("fv: model registered twice: " + m.Name)
	}
	registry[m.Name] = m
}

// Registered returns every registered model sorted by name.
func Registered() []Model {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Model, 0, len(registry))
	for _, m := range registry {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Main is the entry point of the binary kripke check generates; it returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kripke check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o Options
	var model, memLimit string
	var gcPercent int
	var asJSON, verbose, list bool
	fs.StringVar(&model, "model", "", "model name or comma-separated names; empty runs all registered models")
	fs.IntVar(&o.MaxDepth, "max-depth", 0, "maximum depth (0 = unbounded; simulation default 100)")
	fs.IntVar(&o.MaxStates, "max-states", 0, "maximum states (0 = unbounded)")
	fs.DurationVar(&o.Deadline, "deadline", 0, "stop after this long")
	fs.BoolVar(&o.DFS, "dfs", false, "depth-first, serial; traces are not shortest")
	fs.IntVar(&o.Workers, "workers", 0, "worker goroutines (0 = GOMAXPROCS)")
	fs.BoolVar(&o.NoSymmetry, "no-symmetry", false, "ignore declared symmetries")
	fs.BoolVar(&o.POR, "por", false, "static partial-order reduction (safety only)")
	fs.BoolVar(&o.PORValidate, "por-validate", false, "check declared independence at every state")
	fs.BoolVar(&o.SHA256, "sha256", false, "hash states with truncated SHA-256 instead of xxh3")
	fs.IntVar(&o.Simulate, "simulate", 0, "run N random walks instead of exhaustive search")
	fs.Uint64Var(&o.Seed, "seed", 1, "simulation seed")
	fs.IntVar(&gcPercent, "gc-percent", 400, "GOGC for the run")
	fs.StringVar(&memLimit, "mem-limit", "", "soft memory limit, e.g. 8GiB")
	fs.BoolVar(&o.NoDeterminismCheck, "no-determinism-check", false, "skip the double-run determinism check")
	fs.BoolVar(&o.ContinueAfterViolation, "continue", false, "keep exploring after a violation")
	fs.BoolVar(&asJSON, "json", false, "write results as JSON to stdout")
	fs.BoolVar(&verbose, "v", false, "print progress per BFS layer to stderr")
	fs.BoolVar(&list, "list", false, "list registered models and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	debug.SetGCPercent(gcPercent)
	if memLimit != "" {
		n, err := parseBytes(memLimit)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		debug.SetMemoryLimit(n)
	}
	all := Registered()
	if list {
		for _, m := range all {
			fmt.Fprintln(stdout, m.Name)
		}
		return 0
	}
	var run []Model
	if model == "" {
		run = all
	} else {
		byName := map[string]Model{}
		for _, m := range all {
			byName[m.Name] = m
		}
		for _, n := range strings.Split(model, ",") {
			m, ok := byName[strings.TrimSpace(n)]
			if !ok {
				fmt.Fprintf(stderr, "no model %q; registered: %s\n", n, names(all))
				return 2
			}
			run = append(run, m)
		}
	}
	if len(run) == 0 {
		fmt.Fprintln(stderr, "no models registered; call fv.Register in an init function")
		return 2
	}
	if verbose {
		o.Progress = func(p Progress) {
			fmt.Fprintf(stderr, "depth %d: %d states, frontier %d, %s\n", p.Depth, p.States, p.Frontier, p.Elapsed.Round(time.Millisecond))
		}
	}
	code := 0
	var results []Result
	for _, m := range run {
		r := Run(m, o)
		results = append(results, r)
		if !asJSON {
			fmt.Fprint(stdout, RenderResult(r))
		}
		switch {
		case r.Error != "":
			code = 2
		case len(r.Violations) > 0 && code == 0:
			code = 1
		}
	}
	if asJSON {
		if err := WriteJSON(stdout, results...); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	return code
}

func names(ms []Model) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return strings.Join(out, ", ")
}

func parseBytes(s string) (int64, error) {
	units := []struct {
		suffix string
		mult   int64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0, fmt.Errorf("bad size %q", s)
			}
			return int64(n * float64(u.mult)), nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad size %q (use e.g. 8GiB)", s)
	}
	return n, nil
}

# kripke: PRD + EDD

`kripke` is an explicit-state model checker for Go. You write a system as plain Go: a state struct, a few action functions, a few predicates. `kripke` walks every reachable state inside a bound, checks the predicates on each one, and prints the shortest path to a violation as a step-by-step state diff. Named after the Kripke structure, the state graph a model checker walks.

This document is the spec to rebuild it. An agent with this file and Go 1.26 should reproduce the tool and the example models and get the same results.

---

## 1. Problem

Concurrent and distributed systems fail in interleavings nobody tested. "Every item is processed exactly once" holds on every code path and breaks when two workers, a retry and a late message line up in one order. Unit tests pick a handful of interleavings. A model checker tries all of them inside a bound and shows the shortest sequence that breaks the property.

Second problem: drift. A TLA+ or FizzBee spec is a separate drawing of the system and goes stale as the code moves. If the model is Go, the design model written today can become the production package tomorrow, and the checker keeps driving real code.

## 2. Goals and non-goals

Goals:

1. A library (`fv`) and CLI (`kripke`) that explore every reachable state of a Go model, check safety invariants, check liveness under fairness, and render the shortest counterexample.
2. A hard "pure core" rule, enforced by a static lint and a runtime determinism check: checked code has no I/O, clock, randomness, goroutines or mutable globals.
3. A model pattern where one package carries the legacy behaviour, each partial fix and the fixed design as config toggles, so "which fix is sufficient" is a checkable question.
4. `go test` integration so models run in CI.

Non-goals: a spec language (no Starlark, no DSL), disk-backed state sets, proving anything about a database (locks and transactions are modelled as explicit steps), driving code in another language.

Decisions taken, do not reopen: Go-native only; one Go module importable by the systems it checks; the toy examples ship with the tool as the engine's own test suite.

## 3. What the tool enables

- Write a system as a Go struct plus action functions and predicates. No DSL.
- Exhaustive bounded exploration, breadth first, parallel, with identical output for any worker count.
- Safety invariants on every state. Bounded liveness (`Eventually`) on quiescent states. Liveness under weak and strong fairness with fair-cycle detection. Response properties ("whenever A, eventually B").
- Nondeterminism by replay: `c.Pick(n)` inside an action; the engine re-runs the action once per value.
- Symmetry reduction over interchangeable values. Order-free slices via a struct tag.
- Static partial-order reduction from declared reads and writes, with a validator for the declarations.
- Simulation mode: N random walks for models too big to finish.
- A runtime determinism check that catches map-order, clock and hidden-global leaks.
- `fv.Check` fails a test with the rendered trace. `fv.Expect` asserts that a named violation is found, for pinning known bugs.
- A purity lint for the production core package.
- JSON output and a trace pretty-printer.

## 4. Engine design (`fv`)

### 4.1 Public types

```go
type State interface{ Clone() State }   // deep copy; the engine never mutates in place
type Keyer interface{ Key() []byte }    // optional override of state identity

type Model struct {
	Name            string
	Init            func() []State
	Actions         []Action
	Invariants      []Invariant // checked on every state
	Eventually      []Invariant // "eventually always P": quiescent states and fair cycles
	DeadlockIsError bool        // quiescence itself is a violation
	Symmetries      []Symmetry  // fv.Sym(a, b, c): values of one named type
	Responses       []Response  // fv.Resp(name, trigger, response)
}

type Action struct {
	Name          string
	Enabled       func(s State) bool              // nil = always
	Step          func(c *Choice, s State) State  // gets a clone; nil = branch does not apply
	Fair          bool                            // weak fairness
	StrongFair    bool
	Reads, Writes []string                        // top-level field names, for POR; nil = everything
}

type Invariant struct{ Name string; Holds func(State) bool; Reads []string }
```

**Choice:** `Pick(n) int`, `Bool() bool`, `Any[T](c, []T) T`, `Choices() []int`. Replay: run `Step` with an empty log, record every `Pick`, re-run with each untried alternative prefix until the tree is exhausted. No forking, no interpreter.

**Options:** `MaxDepth`, `MaxStates`, `Deadline`, `DFS`, `NoDeterminismCheck`, `ContinueAfterViolation`, `Workers`, `NoSymmetry`, `POR`, `PORValidate`, `SHA256`, `Simulate`, `Seed`, `Progress`.

**Result:** state count, transitions, depth, `Complete`, `CyclesChecked`, violations (each with kind `invariant|eventually|liveness|response|deadlock|determinism|panic`, name, `Trace{Steps}`), timing. `Result.OK()`.

**Helpers:** `fv.Check(t, m, opts) Result` (fails on any violation, renders the trace), `fv.Expect(t, m, opts, kind, name) Violation` (fails unless that named violation is found; scans all violations, so pass `ContinueAfterViolation` to reach non-shortest ones), `fv.Run(m, opts) Result`, `fv.Register(m)` and `fv.Registered()`, `fv.Main(args, stdout, stderr) int`, `fv.Render`, `fv.RenderResult`, `fv.WriteJSON`, `fv.ReadJSON`, `fv.Key(s)`, `fv.Describe(s)`.

### 4.2 State identity

Reflection-based canonical encoding, cached per type as a field plan. Maps sorted by key, pointers followed once, chans and funcs rejected, `fv:"-"` fields skipped (configuration), `fv:"set"` slices encoded order-free. Nil and empty slices or maps encode the same, so a drained queue equals a never-filled one. Hash to 128 bits with xxh3 (`github.com/zeebo/xxh3`); `--sha256` swaps in truncated SHA-256 at the same width. Collision odds over a billion states are about one in ten to the twentieth either way. Symmetry: for N declared values, encode under all N! renamings and keep the smallest. Keep N at 4 or below.

### 4.3 Explorer

- BFS by layers, so the first violation has the shortest trace. Each layer is split into chunks expanded by `Workers` goroutines; dedup is applied in a fixed order so counts, depth and traces are identical for any worker count. DFS is serial.
- Node table: `parent int32`, `action int16`, `cLen int16`, `depth int32`, `cOff int32`, `key`, `props uint64`, `enabled uint64`, `resp uint64`, `quiescent bool`. Choice logs in one shared `[]int32` arena. `seen map[stateKey]int32`. Edges stored for liveness.
- Trace: walk parents to the root, replay the choice logs to rebuild the states, print one state diff per step.
- Determinism check (default on): run every transition twice with the same choice log, compare hashes, fail with the action name on mismatch. Costs 2x step time; `--no-determinism-check` after a clean pass.
- Deadlock: a state with no enabled action; reported when `DeadlockIsError`.
- Limits: depth, states, deadline. An incomplete run reports `liveness cycles not checked`.
- State hygiene warnings at start: map or interface fields in the state type.
- GC knobs: `--gc-percent` (default 400), `--mem-limit`.

### 4.4 Liveness

`Eventually P` is violated by a quiescent state where P fails, or by a reachable cycle containing a state where P fails that is fair: every `Fair` action enabled on all states of the cycle is taken on it, every `StrongFair` action enabled on any state of it is taken on it. A cycle that starves a fair action is not real behaviour and is ignored. After a complete exploration, run iterative Tarjan over the stored edges, filter strongly connected components by fairness, render the trace ending with `loops back to step k`.

Response properties: a trigger state from which a quiescent state or a fair cycle never reaches the response.

### 4.5 Partial-order reduction

Static. Build an independence matrix from `Reads` and `Writes`: two actions are independent when neither writes what the other reads or writes. At each state pick an ample set, a minimal independent closed subset of the enabled actions that leaves the state fresh; else expand all. Safety only. `--por-validate` checks at every state that declared-independent pairs really commute (same key multiset after `ab` and `ba`). Measured gain on the shipped models: zero. Do not build dynamic POR unless a model fails to finish.

### 4.6 Simulation

`--simulate N`: N random walks of `MaxDepth` steps (default 100). Walk i is seeded from `(Seed, i)`, so results do not depend on `Workers`. Invariants are checked along the walk. No completeness claim.

### 4.7 Measured performance (14-core M-series laptop, determinism check on)

| Run | Result |
|---|---|
| Serial throughput | ~60 000 states/s |
| All cores | ~230 000 states/s |
| Largest example, 5.66 M states, 13 M transitions, depth 32 | 23 s, 19 GB peak |
| Same model before cutting redundant nondeterminism, 9.4 M states | 41 s, 28 GB; `--gc-percent 100` 64 s 13 GB; plus `--mem-limit 8GiB` 90 s 9 GB |
| 874 067 states, small state struct | 1 s |

Memory is the limit before time, and most of the peak is garbage-collector slack, not live data. Stated goal: a billion states in a minute. Not there.

Tried and dropped: encoding frontier states into a pointer-free byte arena. Slower on decode and saved nothing, because the frontier is not where memory goes. Not built: disk-backed state set, dynamic POR, a spec language.

## 5. CLI (`cmd/kripke`, cobra)

```
kripke check <pkg> [--model NAME] [--max-depth N] [--max-states N] [--deadline D] [--dfs]
             [--workers N] [--no-symmetry] [--por] [--por-validate] [--sha256]
             [--simulate N] [--seed S] [--gc-percent P] [--mem-limit 16GiB]
             [--no-determinism-check] [--continue] [--json] [-v]
kripke trace <file|->           pretty-print a JSON result
kripke purity <pkgs...> [--policy purity.yaml]
```

`check` resolves the package with `go list`, writes `<pkg>/.kripke-run/main.go` containing `import _ "<pkg>"` and `os.Exit(fv.Main(os.Args[1:], os.Stdout, os.Stderr))`, runs `go run` in the package directory with the flags forwarded, then removes the directory. Models register with `fv.Register` in `init()`. Same trick `go test` uses, so specs stay plain Go packages. The target module must depend on the kripke module, which is automatic when it imports `fv`.

## 6. Purity lint (`purity`)

Static layer of the pure-core rule. `purity.Check(pattern, policy)` and `purity.AssertPure(t, moduleRoot, pattern)`.

- Import denylist, transitive over same-module dependencies: `context`, `crypto/rand`, `database/sql`, `io/fs`, `log`, `log/slog`, `math/rand`, `math/rand/v2`, `net`, `os`, `os/exec`, `os/signal`, `plugin`, `reflect`, `runtime`, `sync`, `sync/atomic`, `syscall`, `testing`, `unsafe`, plus common Kafka, Postgres and gRPC clients. Prefix match on path segments: `net` denies `net/http`, not `network`.
- AST pass: `go` statements, channel operations and `select`, `time.Now`/`Since`/`Tick`-style calls, `unsafe`, `init()`, package-level `var` (sentinel `errors.New` and `var _ =` are allowed).
- `purity.yaml` at the module root: `allow_imports`, `deny_imports`, `allow_globals`, `allow_calls`.
- Run it on the core package only. The model package imports `fv`, which is the engine and impure by design.
- The runtime half is the determinism check in `fv`: the lint catches a clock read on a branch never taken, the runtime check catches map iteration order and hidden mutation through pointers.

Fixtures under `purity/internal/fixtures/{clean,clean_with_dep,dirty,dirty_dep}`.

## 7. Repo layout

```
kripke/
├── go.mod          go 1.26; deps: github.com/spf13/cobra, github.com/zeebo/xxh3, gopkg.in/yaml.v3
├── Makefile        build | install (~/go/bin/kripke) | test | lint (vet + purity on the example core)
│                   | tidy
├── README.md       usage, model-writing rules, performance, every example's results
├── SKILL.md        Claude Code skill (copy to ~/.claude/commands/kripke.md)
├── cmd/kripke/     main.go root.go check.go trace.go purity.go
├── fv/             model.go choice.go explore.go liveness.go por.go simulate.go codec.go key.go
│                   hygiene.go registry.go result.go trace.go testing.go
├── purity/         purity.go policy.go imports.go ast.go golist.go doc.go internal/fixtures/
└── examples/       one package per model, each with <name>.go and <name>_test.go
```

Comment style: one-line doc comments only, no comments inside function bodies, no struct-field comments.

## 8. Model-writing rules

1. State is a struct with `Clone()`. Configuration lives in a `Cfg Config` `` `fv:"-"` `` field: one bool per real mechanism, all false = the fixed design.
2. Actions return `nil` when they do not apply. Nondeterminism only through `c.Pick`, `c.Bool`, `fv.Any`. Never iterate a map in a step.
3. Prefer slices and arrays to maps (a map costs a sort per key and an allocation per clone). Keep a slice sorted or tag it `fv:"set"`, or the checker sees one state as many.
4. `Fair: true` only on system-driven actions (workers, schedulers, services). Clients, operators, outages and duplicate deliveries are unfair.
5. Cut nondeterminism that adds no coverage and write down why. On the largest example, three such cuts (one late item at a time, only one message type redelivered, an outage only while work is queued) took 9.4 M states to 5.8 M with every finding intact.
6. Declare symmetry only when the values are interchangeable everywhere: no action or invariant singles one out, no array is indexed by them.
7. Every model package registers a fixed variant and one variant per toggle. Tests: the fixed design is clean with `Complete && CyclesChecked`; each toggle is pinned with `fv.Expect` and an exact or bounded step count; interaction tests cover "fix A alone" and "fix B alone".
8. Review invariants by hand after delegating a model to another agent. A first draft that tracked membership instead of the items themselves over-counted and fired a false alarm on the fixed design.

Template shape: types, `Config`, `World` with `Clone`, `initial(cfg)`, one function per action, `Model(name, cfg) fv.Model`, `with(cfg, f)` to derive variants, `init()` with the `fv.Register` calls.

Minimal model:

```go
type Sys struct{ N int }

func (s *Sys) Clone() fv.State { c := *s; return &c }

var m = fv.Model{
	Name: "counter",
	Init: func() []fv.State { return []fv.State{&Sys{}} },
	Actions: []fv.Action{{
		Name: "inc",
		Step: func(c *fv.Choice, s fv.State) fv.State {
			st := s.(*Sys)
			if st.N >= 5 { return nil }
			st.N += 1 + c.Pick(2)
			return st
		},
	}},
	Invariants: []fv.Invariant{fv.Inv("n <= 6", func(s fv.State) bool { return s.(*Sys).N <= 6 })},
	Eventually: []fv.Invariant{fv.Inv("done", func(s fv.State) bool { return s.(*Sys).N >= 5 })},
}

func init() { fv.Register(m) }
func TestCounter(t *testing.T) { fv.Check(t, m, fv.Options{}) }
```

## 9. Example models

Each example has a passing model and a broken variant a test expects to fail, so the suite checks that the engine both proves and finds.

| Model | What it checks | Engine feature it exercises |
|---|---|---|
| `twophase` | two-phase commit, 3 symmetric participants; finds the unilateral-abort bug when the invariant is weakened | symmetry, safety |
| `philosophers` | dining philosophers; deadlock shows as a quiescent state; the polite variant eats | `DeadlockIsError`, `Eventually` |
| `peterson` | Peterson's mutex; bounded rounds for `Eventually`, unbounded for the response "waiting is eventually critical" | fairness, response properties, fair cycles |
| `transfer` | bank transfers over 3 symmetric accounts; a non-atomic read-modify-write loses an update | symmetry, interleaving |

Symmetry wins measured with `--no-symmetry`: two-phase commit and transfers shrink several times; a 4-value symmetric model went from 330 656 states to 15 439, a 21x cut against a theoretical 24. Wall time barely moves at N=4 (24 encodings per state eat the saving); the win is memory and completeness.

Beyond the toys, the shipped repo carries ten domain models of a settlement system (a pure core package plus the world around it: queues, workers, clients, operators, retries, outages, duplicate deliveries). They are the real proof that the pattern in section 8 works: every historical incident of that system is reproduced as a shortest trace by switching one toggle on, and each fixed design checks clean and complete. Their details belong to that system, not to the tool.

## 10. Build history

1. Engine with replay choice, canonical key, BFS, trace, determinism check, `fv.Check`; purity lint; CLI; `twophase`; first domain model.
2. Parallel BFS and liveness under fairness. Slice-based states and pointer-free nodes. Cached encoding plan, hygiene warnings, GC knobs.
3. Symmetry, `fv:"set"`, philosophers, peterson, transfer. Nondeterminism cuts on the largest model. Response properties. Simulation mode. xxh3-128 hashing. Static POR with validator.
4. Incident sweep: seven more domain models, each from a written incident record, three of them delegated to subagents with the template and the rule "fixed design clean, one toggle per mechanism".

What the engine caught that review did not: a map iteration leak in a step (determinism check); a fix plan written by hand that was wrong on the simplest case; an "either fix alone suffices" claim that held for one fix and not the other; two fixes that are each safe alone and unsafe in one combination.

## 11. Verification

- `go test ./...` green: 9 explorer, 4 choice, 2 hygiene, 5 key, 4 POR, 6 liveness, 4 response, 3 simulate, 4 symmetry, 9 purity tests; 3 to 12 tests per example package pinning step counts.
- `make lint`: `go vet` plus `kripke purity` on the example core package.
- Determinism: same state counts and traces for any `--workers` value.
- Planted `time.Now()` in a pure package fails both the lint and the determinism check.

## 12. Rebuild checklist

1. `go mod init <module>`, deps: `github.com/spf13/cobra`, `github.com/zeebo/xxh3`, `gopkg.in/yaml.v3`. Package resolution shells out to `go list`.
2. `fv/model.go` types from 4.1; `choice.go` replay; `codec.go` plan-cached encoding, set tag, symmetry renamers, xxh3 and SHA-256; `explore.go` layered parallel BFS with ordered dedup, node table, determinism check, limits; `trace.go` replay and diff render, JSON; `liveness.go` Tarjan plus fairness filter and responses; `por.go`; `simulate.go`; `hygiene.go`; `registry.go` and `Main`; `testing.go` with `Check` and `Expect`.
3. `purity/` with the denylist in section 6 and the yaml policy.
4. `cmd/kripke` with the flags in section 5 and the generated-main trick.
5. `examples/twophase` first: it must find the abort bug when the invariant is weakened and pass when correct. Then philosophers, peterson, transfer, each with its broken variant.
6. First real model: a pure core package under `purity`, a model package around it, one config toggle per mechanism, tests pinning every trace.

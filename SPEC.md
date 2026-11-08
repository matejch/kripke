# kripke: PRD + EDD (v2)

`kripke` is an explicit-state model checker for Go. You write a system as plain Go: a state struct, a few action functions, a few predicates. `kripke` walks every reachable state inside a bound, checks the predicates on each one, and prints the shortest path to a violation as a step-by-step state diff. Named after the Kripke structure, the state graph a model checker walks.

This document is the spec to rebuild it. An agent with this file and Go 1.25 or later should reproduce the engine, the CLI, the lint and the five example packages, and get the state counts and step counts in section 9 exactly. Timings in 4.8 depend on the machine.

### Changes from v1

v1 described a tool whose largest numbers came from private domain models. v2 is the version that was rebuilt and measured from this file. The changes:

- **Reproducibility.** The private settlement models and their numbers are gone. A small public `settle` example (pure core plus world model) replaces them, and the performance table uses a public benchmark.
- **Strong fairness.** It is now defined as an iterative SCC refinement. A single fairness filter over SCCs is unsound for strong fairness (4.5).
- **POR.** It now has the visibility condition and a BFS-safe cycle proviso, and refuses to run with liveness properties, symmetry or DFS (4.6).
- **Semantics.** "Enabled" for fairness, the progress assumption, the meaning of `Eventually`, and when symmetry actually collapses states are now explicit.
- **Missing pieces.** `Symmetry`, `Response`, `Inv`, `Sym` and `Resp` are defined. The hard limits are stated and enforced.
- **CLI.** It no longer writes into the source tree: it uses `go build -overlay` into a temporary directory, which also keeps exit codes intact.
- **Determinism check.** Its limits are stated. It does not reliably catch clock reads; the lint does.
- **Goals.** The "billion states in a minute" goal is dropped (section 2).
- **Skill path.** The skill installs to `~/.claude/skills/kripke/SKILL.md`.

---

## 1. Problem

Concurrent and distributed systems fail in interleavings nobody tested. "Every item is processed exactly once" can hold on every code path and still break when two workers, a retry and a late message line up in one order. Unit tests pick a handful of interleavings. A model checker tries all of them inside a bound and shows the shortest sequence that breaks the property.

Second problem: drift. A TLA+ spec is a separate drawing of the system and goes stale as the code moves. If the decision logic is a pure Go package that both production and the model call, that half of the model cannot drift. **The world around it (queues, databases, retries, outages) is still a hand-written model and can drift exactly like a TLA+ spec.** The countermeasure is procedural: every incident record becomes a toggle whose shortest trace must reproduce the incident, so a world model that no longer reproduces known incidents fails its tests.

## 2. Goals and non-goals

Goals:

1. A library (`fv`) and CLI (`kripke`) that explore every reachable state of a Go model, check safety invariants, check liveness under fairness, and render the shortest counterexample.
2. A hard "pure core" rule: checked production code has no I/O, clock, randomness, goroutines or mutable globals. A static lint (`purity`) enforces it reliably. A runtime determinism check catches some leaks the lint cannot see (4.4).
3. A model pattern where one package carries the legacy behaviour, each partial fix and the fixed design as config toggles, so "which fix is sufficient" is a checkable question.
4. `go test` integration so models run in CI.

Non-goals:

- A spec language: no Starlark, no DSL.
- Disk-backed state sets.
- Proving anything about a database: locks and transactions are modelled as explicit steps.
- Driving code in another language.
- Dynamic POR.
- Billions of states. Peak memory is about 320 bytes per state (4.8), so a 10^9-state run needs about 300 GB of RAM. Reaching that needs a disk-backed or compressed state set, which is a non-goal. Practical ceiling: tens of millions of states on a laptop.

Decisions taken, do not reopen:

- Go-native only.
- One Go module that the systems it checks can import.
- The examples ship with the tool as the engine's own test suite.
- `Eventually` keeps its name for compatibility with existing models; its meaning is "eventually always" (4.5).

## 3. What the tool enables

- Write a system as a Go struct plus action functions and predicates.
- Exhaustive bounded exploration: breadth first and parallel, with identical output for any worker count.
- Safety invariants on every state.
- `Eventually` ("eventually always P") on quiescent states and fair cycles, under weak and strong fairness.
- Response properties ("whenever A, eventually B").
- Nondeterminism by replay: `c.Pick(n)` inside an action, and the engine re-runs the action once per value.
- Symmetry reduction over interchangeable values. Order-free slices via a struct tag.
- Static partial-order reduction (safety only) from declared reads and writes, with a validator for the declarations.
- Simulation mode: N random walks for models too big to finish.
- A runtime determinism check that catches map-order leaks, shallow `Clone`, and some hidden-global and clock leaks.
- `fv.Check` fails a test with the rendered trace. `fv.Expect` asserts that a named violation is found, for pinning known bugs.
- A purity lint for the production core package.
- JSON output and a trace pretty-printer.

## 4. Engine design (`fv`)

### 4.1 Public API

```go
type State interface{ Clone() State }   // deep copy; the engine never mutates in place
type Keyer interface{ Key() []byte }    // optional override of state identity; see 4.2

type Model struct {
	Name            string
	Init            func() []State
	Actions         []Action
	Invariants      []Invariant  // checked on every state
	Eventually      []Invariant  // "eventually always P", see 4.5
	DeadlockIsError bool         // a quiescent state is itself a violation
	Symmetries      []Symmetry   // fv.Sym[T](a, b, c)
	Responses       []Response   // fv.Resp(name, trigger, response)
}

type Action struct {
	Name          string
	Enabled       func(s State) bool              // nil = always; a cheap pre-filter only
	Step          func(c *Choice, s State) State  // gets a clone; returns it (or another state), nil = branch does not apply
	Fair          bool                            // weak fairness
	StrongFair    bool                            // strong fairness
	Reads, Writes []string                        // top-level field names, for POR; nil = everything
}

type Invariant struct{ Name string; Holds func(State) bool; Reads []string } // Reads nil = everything
type Response  struct{ Name string; Trigger, Response func(State) bool }
type Symmetry  struct{ /* unexported: a named type and its values */ }

func Inv(name string, holds func(State) bool, reads ...string) Invariant
func Resp(name string, trigger, response func(State) bool) Response
func Sym[T comparable](vals ...T) Symmetry  // T: a defined integer or string type, e.g. type RM int
```

**Choice:** `Pick(n) int` (n ≥ 1; `Pick(1)` returns 0 and records nothing), `Bool() bool`, `Any[T](c, []T) T`, `Choices() []int`. Replay works like this:

1. Run `Step` with an empty prefix, picking 0 at every new choice point and recording each value and its arity.
2. Find the last recorded position whose value can still be incremented.
3. Re-run with the log up to that position, plus the incremented value, as a forced prefix.
4. Repeat until no position can be incremented.

There is no forking and no interpreter. A replayed value that is out of range for the arity now requested is a determinism violation.

**Options:**

| Field | Meaning |
|---|---|
| `MaxDepth`, `MaxStates`, `Deadline` | Bounds (0 = none). Simulation defaults `MaxDepth` to 100. |
| `DFS` | Serial depth-first search. |
| `NoDeterminismCheck` | Skip the double run in 4.4. |
| `ContinueAfterViolation` | Keep exploring after a violation. |
| `Workers` | Number of workers (0 = GOMAXPROCS). |
| `NoSymmetry` | Ignore declared symmetries. |
| `POR`, `PORValidate` | Partial-order reduction and its validator (4.6). |
| `SHA256` | Hash with truncated SHA-256 instead of xxh3. |
| `Simulate`, `Seed` | Number of random walks and their seed (4.7). |
| `Progress func(Progress)` | Called once per BFS layer. |

**Result:**

| Field | Meaning |
|---|---|
| `Model` | Model name. |
| `States`, `Transitions`, `Depth` | Counts. |
| `Complete` | No bound was hit and the run did not stop early on a violation. |
| `CyclesChecked` | True when `Complete` and liveness analysis ran. Vacuously true for a model without liveness properties. |
| `Simulated` | Number of random walks run. |
| `Violations` | Each has a `Kind`, a `Name`, a `Detail` and a `Trace{Steps, LoopsTo, UpToSym}`. |
| `Warnings` | State hygiene warnings (4.3). |
| `Incomplete` | Why the run is incomplete. |
| `Error` | Model or option error. |
| `Elapsed` | Wall time. |

`Result.OK()` means no violations and no error. Violation kinds: `invariant|eventually|liveness|response|deadlock|determinism|panic|por`. Each `(kind, name)` pair is reported at most once, and the first occurrence found is the shortest one.

**Helpers:** `Run(m, o) Result` never panics: model errors land in `Result.Error`, and panics inside actions or predicates become `panic` violations. The others:

- `Check(t, m, o) Result` fails the test on any violation or error.
- `Expect(t, m, o, kind, name) Violation` fails unless that violation is found. It scans all violations, so pass `ContinueAfterViolation` to reach ones that are not the first.
- `Violation.Steps()`.
- `Register(m)` (panics on a duplicate name), `Registered()`, `Main(args, stdout, stderr) int`.
- `Render`, `RenderResult`, `WriteJSON(w, results...)`, `ReadJSON(r)`.
- `Key(s)` returns the canonical bytes without symmetry. `Describe(s)` returns one `path = value` line per leaf.

**Validated at start (an error, not a panic):**

- Name, `Init`, and at least one action are present.
- Action and property names are unique.
- Limits: at most 64 actions (`enabled` is a uint64), 64 `Eventually` properties (`props` uint64) and 32 responses (`resp` uint64, two bits each).
- `Reads` and `Writes` name real top-level fields.
- A symmetry type is a defined integer or string type with at least two values.
- `Keyer` together with `Symmetries` is an error: a Keyer must canonicalize itself.
- The POR restrictions in 4.6.

At most 32 767 initial states (the init index is stored in the `int16` action field). Limits enforced during the run: about 2.1 B states (`int32` node index), 32 767 choices per step (`int16`), and a choice arena of at most 2^31 entries.

### 4.2 State identity

The encoding is reflection-based and canonical, with one compiled encoder ("plan") per type:

- **Plan cache:** a global map guarded by a lock, plus a lock-free per-worker copy. Without the per-worker copy, lock contention made parallel runs slower than serial ones.
- **Integers:** varint. **Floats:** their bit patterns. **Strings:** length-prefixed.
- **Maps:** encoded as a sorted multiset of encoded (key, value) pairs.
- **`fv:"set"` slices:** encoded as a sorted multiset of encoded elements.
- **Nil vs empty:** nil and empty slices or maps encode the same, so a drained queue equals a never-filled one.
- **Pointers:** followed, nil encoded as 0. Pointer depth is capped at 10 000 to catch cyclic states.
- **Interfaces:** the dynamic type name plus the value.
- **Rejected:** chans, funcs and unsafe pointers.
- **Skipped:** fields tagged `fv:"-"` (configuration).

The hash is 128 bits with xxh3 (`github.com/zeebo/xxh3`). `SHA256` swaps in truncated SHA-256 at the same width. Collision odds over 10^9 states are about 1.5·10^-21.

**Symmetry:** for symmetries of sizes N1, N2, …, encode the state under every combination of renamings (N1!·N2!·…) and hash the lexicographically smallest encoding. Keep each N at 4 or below. **Renaming changes values, not positions.** States therefore only collapse when the symmetric values live in `fv:"set"` slices or map keys. With a positional slice or array of processes, symmetry does nothing: the shipped test pins 16 states with and without it. Symmetry is sound only if every action, invariant, `Eventually` predicate, response and fairness flag treats the values identically.

**Keyer:** the engine uses `Key()` verbatim. It is the model's job to make that key canonical, which is why it conflicts with `Symmetries`.

### 4.3 Explorer

**BFS by layers**, so the first violation found has the shortest trace:

1. Split the layer into chunks of 128 states.
2. Workers expand chunks in parallel. Each worker has its own encoder and plan cache.
3. A single merger takes chunk results strictly in chunk order, so dedup, node numbering, counts and traces are identical for any worker count.
4. At most 4×workers chunks are in flight, which bounds the memory held in pending successors.
5. Each frontier state is released as soon as its expansion is merged.

DFS is serial and not shortest. With `MaxDepth` it may report incomplete runs that BFS would finish.

**Expansion of one state:**

1. For each action whose `Enabled` passes, enumerate every choice branch and collect the non-nil successors.
2. **An action is enabled at a state iff it produced at least one successor.** Fairness uses this definition. An action with `Enabled == nil` whose `Step` always returns nil is disabled, not "enabled forever".
3. Evaluate invariants, `Eventually` bits and response bits on each successor, in the worker.

**Merge:** for each successor in (action, branch) order:

- Count the transition.
- If the key is already seen, record the edge (only when the model has liveness properties).
- Otherwise add a node, push the state onto the next layer, and report its failing invariants.

A state with no successors is **quiescent**. It is reported as a `deadlock` when `DeadlockIsError`, and as `eventually` for each `Eventually` property false there.

**Node table:** `parent int32, action int16, cLen int16, depth int32, cOff int32, key [16]byte, props, enabled, resp uint64, quiescent bool`, 64 bytes. Choice logs live in one shared `[]int32` arena. The seen set is `map[[16]byte]int32`. Edges are `(from, to int32, act int16)`, sorted and deduplicated into CSR form for liveness.

**Bounds:**

- `MaxDepth`: nodes at the maximum depth are still expanded. Their edges to known states are recorded, and the run is marked incomplete only if a new state would have been added. A bound equal to the diameter therefore gives a complete run.
- `MaxStates` and `Deadline` stop adding states and mark the run incomplete.
- With `ContinueAfterViolation`, a state whose expansion fails (panic, determinism, POR validation) is reported and not expanded, so the run is marked incomplete and liveness is not checked.
- Stopping at the first violation also leaves the run incomplete, and liveness is not checked.

**Hygiene warnings** at start: map fields (a sort per key on every encode, an allocation per clone) and interface fields.

**GC knobs** apply to the CLI only: `--gc-percent` (default 400) and `--mem-limit`.

### 4.4 Determinism check (default on)

Every branch runs twice from two fresh clones with the same choice log, and the two raw hashes are compared. On a mismatch, the parent is re-encoded. If it changed, the error is "action X modified the state it was given; Clone is not a deep copy". Otherwise it is "different results on two runs". After every action, the parent is also re-encoded and compared, to catch shallow `Clone` even when both runs agree. The check costs about 2× step time. Use `--no-determinism-check` after a clean pass.

What it catches reliably:

- Map iteration order that leaks into a step (Go randomizes it per range).
- A shallow `Clone`.
- A hidden global that a step mutates.

What it does **not** catch reliably:

- **Clock reads.** Both runs usually fall in the same clock tick, so a planted `time.Now()` passes. The purity lint is the guard for clocks.
- **Randomness seeded per process.**

### 4.5 Liveness

**Semantics.** A behaviour is either an infinite path or a finite path ending in a quiescent state, which then stutters forever. **Non-quiescent states always progress**: the engine never assumes a system that can move simply stops. Unfair actions on acyclic paths therefore still happen. Fairness only matters on cycles.

A cycle is fair iff:

- **Weak:** every `Fair` action that is enabled on all states of the cycle is taken on the cycle.
- **Strong:** every `StrongFair` action that is enabled on some state of the cycle is taken on the cycle.

Fairness is per action, not per branch. To give each process its own guarantee, give each process its own actions.

**`Eventually P` means "eventually always P" (◇□P).** It is violated by a quiescent state where P is false (kind `eventually`), or by a reachable fair cycle that contains a state where P is false (kind `liveness`). For bounded, terminating models this is "P holds at the end". For "whenever A, eventually B" use a response. Do not try to express ◇P on cyclic models with `Eventually`.

**Algorithm**, run only after a complete exploration with edges stored:

1. Start with all allowed nodes as one region.
2. Run iterative Tarjan restricted to the region.
3. For each SCC that has a cycle (more than one node, or a self-loop):
   - **Discard** it if a weak-fair action is enabled on all of its states and never taken inside it. No sub-cycle can then be fair.
   - **Refine** it if a strong-fair action is enabled somewhere in it but never taken inside it. Remove the states where any such action is enabled, give the rest a new region, and repeat from step 2 on that region.
   - Otherwise **accept** it. The cycle that covers the whole SCC is fair.

**Do not shortcut step 3 with a single filter.** An SCC can fail the strong check while a smaller cycle inside it, one that avoids the enabling states, is fair. A single filter reports a pass for a broken model. `TestStrongFairnessFindsTheFairSubcycle` pins this case.

**Eventually:** a violation is any accepted component that contains a ¬P node. Pick the lowest node index, which is the shallowest.

**Responses** (Trigger T, Response R):

1. Restrict the graph to ¬R nodes.
2. Compute the accepted components inside that subgraph, so an edge into an R state does not count as "taken".
3. The bad set is those components plus the quiescent ¬R nodes.
4. Search backwards within ¬R from the bad set.
5. The violation is the first node with T and ¬R from which the bad set is reachable.

**Rendering a lasso:**

1. The tree path to the cycle entry, replayed from the stored choice logs.
2. A loop built to be fair, not merely short. Within the component it passes through:
   - an edge of every weak-fair action enabled everywhere;
   - a state where each other weak-fair action is disabled;
   - an edge of every strong-fair action enabled anywhere;
   - and back to the entry.
3. Non-tree edges are replayed by re-expanding the current concrete state and taking the branch whose canonical key matches the target. This keeps the trace a genuine concrete execution, even under symmetry.
4. If the loop returns to a renaming of the entry state, the trace says `loops back to step k (up to symmetry)`.

### 4.6 Partial-order reduction (safety only)

Static. Two actions are dependent if either one writes a field the other reads or writes, where nil means "everything". Reads must include every field read by `Enabled` and by the guard in `Step`. The ample candidates are the connected components of the dependency graph, ordered smallest first. A state uses component C as its ample set iff:

- **C0/C1:** every action in C produces a successor at that state. Actions outside C are independent of all of C by construction.
- **C2 (visibility):** no action in C writes a field read by any invariant. `Invariant.Reads == nil` makes every writing action visible.
- **C3 (cycle proviso, BFS-safe):** if any ample successor is already in the seen set at merge time, the state is queued and fully expanded in a second ordered pass of the same layer. Every cycle in the reduced graph contains a dedup-hit edge, so every cycle contains a fully expanded state.

**Refusals (`Result.Error`):** `--por` with `Eventually` or `Responses` (the proviso does not preserve liveness), with symmetry (unverified combination), or with DFS or simulation.

**`--por-validate`:** at every fully expanded state, each declared-independent pair (a, b) that is enabled must have a not disable b and b not disable a, and the multisets of keys after `ab` and after `ba` must be equal. A failure is reported as a violation of kind `por`.

**Honest note:** an action counts as enabled only by running it, so POR saves states, not step executions. Realistic models declare few disjoint field sets. POR exists for memory relief on models with genuinely independent subsystems. Do not build dynamic POR.

### 4.7 Simulation

`--simulate N`: N random walks of up to `MaxDepth` steps (default 100). Walk i uses PCG seeded with `(Seed, i)`, so results do not depend on `Workers`.

- Each step expands every action fully and picks a successor uniformly.
- Invariants are checked along the walk. Deadlock and `Eventually` are checked at quiescence.
- The reported violation is the one from the lowest-numbered failing walk. With `ContinueAfterViolation`, each (kind, name) pair is still reported once.
- There is no completeness claim and no cycle analysis.

### 4.8 Measured performance

Apple M4, 10 cores, Go 1.25, determinism check on. Grid model: 7 independent `int8` counters 0..9, a slice-backed state, 10 M states, 63 M transitions.

| Run | Time | Peak RSS |
|---|---:|---:|
| All cores, GOGC 400 | 7.4 s | 3.2 GB |
| All cores, GOGC 100 | 5.5 s | 3.2 GB |
| All cores, edges stored (liveness) | 10.0 s | 4.2 GB |
| 1 M states, 1 worker / all cores | 1.7 s / 0.48 s | |

Peak memory is about 320 B per state and is mostly **live** data: the node table (64 B), the seen-map (about 40 B plus growth doubling), the frontier and the choice arena. Lowering GOGC did not lower the peak here. The v1 claim that "most of the peak is GC slack" does not hold at this scale. Larger state structs grow the frontier term. Throughput drops with state size, with symmetry (N! encodings per state) and with the determinism check (2×).

The bottleneck order is: the serial merge, then memory bandwidth, then GC. Tried and kept: a per-worker plan cache, which turned a parallel slowdown (0.34 M/s, slower than serial) into a 5× speedup. Not built: a disk-backed state set, dynamic POR, a spec language.

## 5. CLI (`cmd/kripke`, cobra)

```
kripke check <pkg> [--model NAME[,NAME]] [--max-depth N] [--max-states N] [--deadline D] [--dfs]
             [--workers N] [--no-symmetry] [--por] [--por-validate] [--sha256]
             [--simulate N] [--seed S] [--gc-percent P] [--mem-limit 16GiB]
             [--no-determinism-check] [--continue] [--json] [-v] [--list]
kripke trace <file|->           pretty-print a JSON result written by check --json
kripke purity <pkgs...> [--policy purity.yaml]
```

`check` does the following:

1. Resolve `<pkg>` with `go list -json` to get its import path and module directory. A main package is an error.
2. Write `main.go` into `os.MkdirTemp`. It imports `_ "<pkg>"` and calls `os.Exit(fv.Main(os.Args[1:], os.Stdout, os.Stderr))`.
3. Map a virtual `<module>/kripke_run_<pid>/main.go` to that file through a `-overlay` JSON file. Run `go build -overlay … -o <tmp>/kripke-model` in the module directory.
4. Run the binary with the flags forwarded, in the package directory.
5. Remove the temporary directory.

Nothing is written into the source tree, concurrent runs do not collide, and a crash leaves only an OS temp directory. A built binary (rather than `go run`) keeps the exit code: `go run` turns any non-zero exit into 1. The target module must require the kripke module, which is automatic once it imports `fv`. Models register with `fv.Register` in `init()`. With no `--model`, every registered model runs, including broken variants, so expect exit code 1 on example packages.

Exit codes: 0 clean, 1 violation (or purity findings), 2 error, including a model binary killed by a signal (OOM, Ctrl-C).

## 6. Purity lint (`purity`)

The static half of the pure-core rule. API:

```go
purity.Check(dir, pattern string, p Policy) ([]Finding, error)
purity.AssertPure(t, moduleRoot, pattern)    // loads moduleRoot/purity.yaml
purity.LoadPolicy(path)                      // a missing file gives the zero Policy; unknown keys are errors
```

`Finding{Pkg, File, Line, Col, Rule, Msg}` with rules `import|go|chan|select|clock|unsafe|init|global`.

- **One `go list -deps -json` call.** The import and AST rules apply to the target packages and to every dependency **in the same module**, transitively. A transitive hit names the chain.
- **Import denylist:** `context`, `crypto/rand`, `database/sql`, `io/fs`, `log`, `log/slog`, `math/rand`, `math/rand/v2`, `net`, `os`, `os/exec`, `os/signal`, `plugin`, `reflect`, `runtime`, `sync`, `sync/atomic`, `syscall`, `testing`, `unsafe`. Also common Kafka clients (segmentio, sarama, confluent, franz-go), Postgres clients (lib/pq, pgx) and `google.golang.org/grpc`. Matching is a prefix on whole path segments: `net` denies `net/http`, not `network`. `time` is allowed; clock calls are caught by the AST pass.
- **AST pass** (non-test files only):
  - `go` statements, channel types, sends, receives and `select`;
  - `time.Now`, `Since`, `Until`, `Tick`, `After`, `AfterFunc`, `NewTimer`, `NewTicker` and `Sleep`, with renamed and dot imports resolved;
  - any use of `unsafe`;
  - `func init()`;
  - package-level `var`. Allowed: `var _ = …`, sentinels whose value is `errors.New` or `fmt.Errorf`, and names in `allow_globals`.
- **`purity.yaml`** at the module root: `allow_imports`, `deny_imports`, `allow_globals` (`Name` or `importpath.Name`) and `allow_calls` (e.g. `time.Since`).
- **Scope:** run it on the core package only. The model package imports `fv`, which is impure by design.
- **Fixtures:** `purity/internal/fixtures/{clean,clean_with_dep,dirty,dirty_dep}`.

## 7. Repo layout

```
kripke/
├── go.mod          module github.com/matejch/kripke; go 1.25; deps: github.com/spf13/cobra, github.com/zeebo/xxh3, gopkg.in/yaml.v3
├── Makefile        build | install (~/go/bin/kripke) | test | lint (vet + gofmt + purity on the examples' core packages) | tidy | bench
├── purity.yaml     empty policy
├── README.md       usage, model example, every example's results, performance
├── SKILL.md        Claude Code skill; install to ~/.claude/skills/kripke/SKILL.md
├── cmd/kripke/     main.go root.go check.go trace.go purity.go
├── fv/             model.go choice.go codec.go key.go explore.go liveness.go por.go simulate.go
│                   hygiene.go registry.go result.go trace.go testing.go (+ tests, bench_test.go)
├── purity/         purity.go policy.go imports.go ast.go golist.go doc.go internal/fixtures/
└── examples/       twophase philosophers peterson transfer settle settle/core
```

Comment style: one-line doc comments only, no comments inside function bodies, no struct-field comments.

## 8. Model-writing rules

1. State is a struct with `Clone()` that deep-copies every slice and map. Configuration lives in a `Cfg Config` `` `fv:"-"` `` field: one bool per real mechanism, and all false is the fixed design.
2. Actions return nil when they do not apply. Nondeterminism only through `c.Pick`, `c.Bool` and `fv.Any`. Never iterate a map in a step.
3. Prefer slices and arrays to maps. Keep a slice sorted, or tag it `fv:"set"`, or the checker sees one state as many.
4. `Fair: true` only on system-driven actions (workers, schedulers, services). Clients, operators, crashes, outages and duplicate deliveries are unfair and get a **budget** (a counter in the state), so they cannot loop forever and drown liveness in unfair cycles.
5. Fairness is per action. One action per process when each process needs its own guarantee.
6. Cut nondeterminism that adds no coverage, and write down why.
7. Declare symmetry only when the values are interchangeable everywhere: no action, invariant, property or fairness flag singles one out. Keep them in `fv:"set"` slices, or the reduction does nothing.
8. Every model package registers a fixed variant and one variant per toggle. Tests:
   - the fixed design is clean with `Complete && CyclesChecked`;
   - each toggle is pinned with `fv.Expect` and an exact step count;
   - interaction tests cover "fix A alone" and "fix B alone".
9. Review invariants by hand after delegating a model to another agent. They must track the items themselves, not a proxy. A first draft that tracked membership instead of items over-counted and fired a false alarm on the fixed design.

Template shape:

- types and `Config`;
- `World` with `Clone`;
- `initial(cfg)`;
- one function per action (a closure over the process index for per-process actions);
- `Model(name, cfg) fv.Model`;
- `with(cfg, f)` to derive variants;
- `init()` with the `fv.Register` calls.

## 9. Example models

Each example has a passing model and at least one broken variant, so the suite checks that the engine both proves and finds. The numbers below must match on rebuild.

| Package | Variants: result | Exercises |
|---|---|---|
| `twophase` | `twophase`: 80 states, clean (288 without symmetry). `twophase-unilateral-abort` (a prepared RM may abort on its own): invariant `consistent`, 9 steps. | symmetry over `RM` in set slices, safety, `Eventually` at quiescence |
| `philosophers` (3) | One meal each: `philosophers` clean (96 states); `philosophers-naive` (everyone takes left first): `eventually` at the deadlock, 6 steps. Eating forever with `DeadlockIsError`: `philosophers-forever` clean with strong fairness on fork-taking; `-naive`: deadlock, 6 steps; `-weak-fairness`: response `hungry 0 eventually eats`, a 5-step lasso where philosopher 2 keeps taking fork 0. | `DeadlockIsError`, `Eventually`, responses, **weak vs strong fairness** |
| `peterson` | `peterson-bounded` (2 rounds each): clean, 90 states. `peterson` (forever, with an unfair `think` self-loop): response `waiting is eventually critical` holds under weak fairness, 20 states. `-turn-before-flag`: invariant `mutual exclusion`, 6 steps. `-no-fairness`: response violated by a 3-step lasso, P0 waits while P1 thinks forever. | fairness, responses, fair cycles, self-loops |
| `transfer` | 3 symmetric accounts, 2 workers, 2 transfers each. `transfer` (atomic): clean, 28 states. `transfer-non-atomic` (read, then write): invariant `money conserved`, 4 steps, 616 states (2 708 without symmetry). | symmetry, interleaving |
| `posttrade` + `posttrade/core` | 4 trades (2 EUR, 2 USD). Requests: one client batch (any subset of IDs), one client currency request, one end-of-day batch (fair, so it always happens). Jobs select, optionally claim, then settle one trade per step; one crash re-queues a job. `posttrade`: clean, 125 337 states. `-no-claim` and `-no-status-check`: each clean, so either guard alone suffices. `-legacy` (neither): invariant `settled at most once`, 6 steps (a batch and a currency request both pick trade 3). `-no-claim-release`: `eventually` `every trade settled`, 8 steps (end-of-day claims everything, crashes, and its retry finds nothing open). | overlapping request scopes, guards that each suffice, stranded claims |
| `settle` + `settle/core` | Pure ledger core, a queue, 2 workers, crash budget 1. `settle`: clean, 84 states. `-no-idempotency`: invariant `settled at most once`, 5 steps. `-ack-before-commit`: `eventually` `every instruction settled`, 7 steps. `-legacy` (both): only the lost instruction, because acking early hides the duplicate bug. | the section-8 pattern, the purity lint on a real core, fix-interaction tests |

## 10. Build history (of v1, kept for context)

1. Engine with replay choice, canonical key, BFS, trace, determinism check, `fv.Check`; purity lint; CLI; `twophase`; first domain model.
2. Parallel BFS and liveness under fairness. Slice-based states and pointer-free nodes. Cached encoding plan, hygiene warnings, GC knobs.
3. Symmetry, `fv:"set"`, philosophers, peterson, transfer. Nondeterminism cuts on the largest model. Response properties. Simulation mode. xxh3-128 hashing. Static POR with validator.
4. Incident sweep: seven more domain models (eight in total), each from a written incident record.

What the engine caught that review did not:

- a map iteration leak in a step (determinism check);
- a hand-written fix plan that was wrong on the simplest case;
- an "either fix alone suffices" claim that held for one fix and not the other;
- two fixes that are each safe alone and unsafe in one combination.

None of those models ship. The `settle` example reproduces the "one fix hides another" shape publicly.

## 11. Verification

- `go test ./...` is green, and green under `-race`. Engine tests:
  - explorer 13
  - choice 4
  - key 5
  - liveness 6
  - response 4
  - POR 4
  - simulate 3
  - symmetry 5
  - hygiene 2
  - regressions 5 (traces built only for the first report of a violation, init limit, failed expansion is incomplete, plan cache not poisoned by an encode error, simulation dedup)
  
  Plus purity 9, and 3 to 7 tests per example package pinning state and step counts.
- `make lint`: `go vet`, gofmt, and `kripke purity ./examples/settle/core`.
- Determinism: identical counts and traces for `Workers` 1 and 7 or 8 (engine and twophase tests), and in simulation.
- Strong-fairness soundness: `TestStrongFairnessFindsTheFairSubcycle` must fail on a single-filter implementation.
- Clock reads: the purity `dirty` fixture must report `time.Now`/`time.Since`. Do not expect the runtime check to catch them (4.4).

## 12. Rebuild checklist

1. `go mod init github.com/matejch/kripke`. Deps: `github.com/spf13/cobra`, `github.com/zeebo/xxh3`, `gopkg.in/yaml.v3`. Package resolution shells out to `go list`.
2. `fv/`, in this order:
   1. `model.go`: the types and validation from 4.1.
   2. `choice.go`: replay.
   3. `codec.go`: plans, set tag, symmetry renamings, the per-worker plan cache.
   4. `key.go`: xxh3 and SHA-256.
   5. `explore.go`: chunked layered BFS with ordered merge, node table, determinism check, bounds.
   6. `trace.go`: replay, choice-matched edge replay, diff render, JSON.
   7. `liveness.go`: Tarjan with iterative strong-fairness refinement, fair-lasso construction, responses.
   8. `por.go`: components, C0–C3, validator.
   9. `simulate.go`, `hygiene.go`, `registry.go` with `Main`, `testing.go`.
3. `purity/` with the rules and denylist in section 6.
4. `cmd/kripke` with the overlay-and-build trick from section 5.
   Build a violation's trace only when it is the first report of that (kind, name); otherwise `--continue` on a model that violates in most states replays a trace per state.
   A plan that fails to compile must be removed from the global cache, or later runs see a nil plan.
5. Examples in the order twophase, transfer, peterson, philosophers, settle. Match every number in section 9.
6. Benchmark (`make bench`, and a 7-counter grid for memory). Record the machine and the numbers in the README.

## 13. Known limits and open questions

- **Memory** is the ceiling: about 320 B per state. Lowering it means a compact open-addressing seen table keyed by the 128-bit hash, and dropping `key` from the node, since it is already the map key. A merge shard per worker would lift the serial-merge ceiling.
- **The ample set rule is coarse:** whole dependency components that are fully enabled. It is sound but rarely useful.
- **Symmetry combined with liveness** trusts the model author on rule 7. Nothing checks that predicates and fairness flags are symmetric. A `--sym-validate` (compare property values across renamings at every state) would be cheap to add.
- **The world model is not checked against reality.** Incident reproduction tests are the only guard (section 1).

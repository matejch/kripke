# kripke

An explicit-state model checker for Go. You write a system as plain Go: a state struct, action
functions and predicates. `kripke` walks every reachable state, checks safety invariants and
liveness under fairness, and prints the shortest path to a violation as a step-by-step state diff.

```
go env -w GOPRIVATE=github.com/matejch/*   # the repo is private
go get github.com/matejch/kripke
make install                       # ~/go/bin/kripke
kripke check ./examples/peterson   # every registered model in the package
go test ./...                      # the same checks as tests
```

## A model

```go
type Sys struct{ N int }

func (s *Sys) Clone() fv.State { c := *s; return &c }

var Counter = fv.Model{
	Name: "counter",
	Init: func() []fv.State { return []fv.State{&Sys{}} },
	Actions: []fv.Action{{
		Name: "inc",
		Step: func(c *fv.Choice, s fv.State) fv.State {
			st := s.(*Sys)
			if st.N >= 5 {
				return nil
			}
			st.N += 1 + c.Pick(2)
			return st
		},
	}},
	Invariants: []fv.Invariant{fv.Inv("n <= 6", func(s fv.State) bool { return s.(*Sys).N <= 6 })},
	Eventually: []fv.Invariant{fv.Inv("done", func(s fv.State) bool { return s.(*Sys).N >= 5 })},
}

func init() { fv.Register(Counter) }

func TestCounter(t *testing.T) { fv.Check(t, Counter, fv.Options{}) }
```

`SPEC.md` is the full design. `SKILL.md` lists the model-writing rules for agents.

## Commands

```
kripke check <pkg> [--model NAME] [--max-depth N] [--max-states N] [--deadline D] [--dfs]
             [--workers N] [--no-symmetry] [--por] [--por-validate] [--sha256]
             [--simulate N] [--seed S] [--gc-percent P] [--mem-limit 8GiB]
             [--no-determinism-check] [--continue] [--json] [-v] [--list]
kripke trace <file|->
kripke purity <pkgs...> [--policy purity.yaml]
```

Exit codes: 0 clean, 1 violation or purity finding, 2 error.

## Examples

Every example has a clean model and broken variants that the tests pin with exact step counts.
Results come from `kripke check ./examples/<pkg> --continue`.

| Model | States | Result |
|---|---:|---|
| `twophase` | 80 (288 without symmetry) | clean, complete, cycles checked |
| `twophase-unilateral-abort` | 122 | invariant `consistent`, 9 steps |
| `philosophers` (one meal each) | 96 | clean |
| `philosophers-naive` | 100 | eventually `everyone ate`, 6 steps (deadlock) |
| `philosophers-forever` (strong fairness) | 42 | clean, no deadlock, hungry 0 eats |
| `philosophers-forever-naive` | 45 | deadlock, 6 steps |
| `philosophers-forever-weak-fairness` | 42 | response `hungry 0 eventually eats`, 5-step lasso |
| `peterson-bounded` | 90 | clean |
| `peterson` | 20 | clean, response holds under weak fairness |
| `peterson-turn-before-flag` | 32 | invariant `mutual exclusion`, 6 steps |
| `peterson-no-fairness` | 20 | response, 3-step lasso |
| `transfer` | 28 (47 without symmetry) | clean |
| `transfer-non-atomic` | 616 (2 708 without symmetry) | invariant `money conserved`, 4 steps |
| `settle` (core + world) | 84 | clean |
| `settle-no-idempotency` | 112 | invariant `settled at most once`, 5 steps |
| `settle-ack-before-commit` | 108 | eventually `every instruction settled`, 7 steps |
| `settle-legacy` (both toggles) | 108 | only the lost instruction |
| `posttrade` (claim + status check + release on crash) | 125 337 | clean |
| `posttrade-no-claim` / `-no-status-check` | 246 721 / 125 337 | clean: either guard alone prevents double settlement |
| `posttrade-legacy` (neither guard) | 3 447 555 | invariant `settled at most once`, 6 steps |
| `posttrade-no-claim-release` | 83 535 | eventually `every trade settled`, 8 steps: a crashed job strands its claims |

`settle` is the pattern for real systems: `examples/settle/core` is pure production logic (it passes
`kripke purity`), and `examples/settle` models the queue, workers and crashes around it. The legacy
variant shows why "which fix is sufficient" needs checking. Acking early hides the duplicate bug, so
fixing the ack order alone brings duplicates back.

## Performance

Apple M4, 10 cores, Go 1.25, determinism check on. Grid model: 7 independent counters 0..9,
10 M states, 63 M transitions.

| Run | Time | Peak RSS |
|---|---:|---:|
| all cores, GOGC 400 (default) | 7.4 s (1.35 M states/s) | 3.2 GB |
| all cores, GOGC 100 | 5.5 s (1.8 M states/s) | 3.2 GB |
| all cores, with liveness edges stored | 10.0 s | 4.2 GB |
| 1 M states, 1 worker | 1.7 s (0.6 M states/s) | |
| 1 M states, all cores | 0.48 s (2.1 M states/s) | |

Memory is about 320 bytes per state at peak and is mostly live data: the node table (64 B), the
seen-map (about 40 B), the frontier states and map growth. GC slack is not the main cost. Larger
state structs cost proportionally more in the frontier. Throughput falls with state size and with
symmetry (N! encodings per state).

## Layout

```
cmd/kripke/   CLI (cobra): check, trace, purity
fv/           engine: model, choice replay, codec, BFS/DFS explorer, liveness, POR, simulation
purity/       static lint for pure core packages, fixtures under internal/fixtures
examples/     twophase, philosophers, peterson, transfer, settle, posttrade (each pattern model has a core/)
```

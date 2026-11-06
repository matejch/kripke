---
name: kripke
description: Write and check Go models with the kripke model checker (package fv). Use when asked to model a concurrent or distributed design, reproduce an incident as a shortest trace, ask which fix is sufficient, or run `kripke check`.
---

# kripke

Install: `make install` in the kripke repo (binary at `~/go/bin/kripke`). Copy this file to
`~/.claude/skills/kripke/SKILL.md`.

## Commands

```
kripke check ./path/to/model [--model NAME] [--continue] [--max-depth N] [--json] [-v]
kripke check ./path/to/model --list
kripke trace result.json
kripke purity ./path/to/core
go test ./path/to/model
```

Exit codes: 0 clean, 1 violation, 2 error (bad flags, model error, build failure).

## Writing a model

1. Pure logic goes in a `core` package that passes `kripke purity`. The model package imports it and `fv`.
2. State: one struct with `Clone()` that deep-copies every slice and map. Configuration lives in
   `Cfg Config` tagged `fv:"-"`, one bool per real mechanism; all false is the fixed design.
3. Actions: `Step(c, s)` gets a clone, mutates it and returns it, or returns nil when it does not
   apply. Nondeterminism only via `c.Pick(n)`, `c.Bool()`, `fv.Any(c, xs)`. Never range over a map in
   a step.
4. Fairness is per action, not per branch. If each process must make progress, give each process its
   own actions (`enter0`, `enter1`), not one action that picks a process.
5. `Fair: true` only for system-driven steps (workers, schedulers). Clients, operators, crashes,
   outages and duplicate deliveries stay unfair, and give them a budget so they cannot loop forever.
6. `Eventually` means "eventually always": violated by a quiescent state or a fair cycle where the
   predicate fails anywhere on the cycle. For "whenever A then eventually B" use `fv.Resp`.
7. Symmetry (`fv.Sym[T](a, b, c)`) only collapses states when values of `T` live in `fv:"set"` slices
   or map keys, and only when no action or property singles one out.
8. Limits: 64 actions, 64 Eventually properties, 32 responses, about 2.1 billion states.
9. Register a fixed variant and one variant per toggle in `init()`. Tests: the fixed design is clean
   with `Complete && CyclesChecked`; each toggle is pinned with `fv.Expect` and an exact step count;
   add "fix A alone" and "fix B alone" tests.
10. Read every invariant yourself before trusting a clean result. Check that it tracks the items
    themselves, not a proxy such as membership.

## Reading results

`OK` with `complete, cycles checked` is a proof within the model. `INCOMPLETE` or `liveness cycles not
checked` proves nothing about liveness. A trace prints the full initial state, then one diff per step;
a lasso ends with `loops back to step k`.

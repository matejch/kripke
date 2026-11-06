package fv

import "time"

// Violation kinds.
const (
	KindInvariant   = "invariant"
	KindEventually  = "eventually"
	KindLiveness    = "liveness"
	KindResponse    = "response"
	KindDeadlock    = "deadlock"
	KindDeterminism = "determinism"
	KindPanic       = "panic"
	KindPOR         = "por"
)

// Result is the outcome of one run.
type Result struct {
	Model         string        `json:"model"`
	States        int           `json:"states"`
	Transitions   int           `json:"transitions"`
	Depth         int           `json:"depth"`
	Complete      bool          `json:"complete"`
	CyclesChecked bool          `json:"cycles_checked"`
	Simulated     int           `json:"simulated,omitempty"`
	Violations    []Violation   `json:"violations,omitempty"`
	Warnings      []string      `json:"warnings,omitempty"`
	Incomplete    string        `json:"incomplete,omitempty"`
	Error         string        `json:"error,omitempty"`
	Elapsed       time.Duration `json:"elapsed_ns"`
}

// Violation is one property failure with its counterexample.
type Violation struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	Trace  Trace  `json:"trace"`
}

// Trace is a path from an initial state; LoopsTo is the step a lasso returns to, or -1.
type Trace struct {
	Steps   []Step `json:"steps"`
	LoopsTo int    `json:"loops_to"`
	UpToSym bool   `json:"up_to_symmetry,omitempty"`
	last    State
}

// Step is one transition and the full state it produced; step 0 is the initial state.
type Step struct {
	Action  string  `json:"action"`
	Choices []int   `json:"choices,omitempty"`
	State   []Field `json:"state"`
}

// Field is one leaf of a described state.
type Field struct {
	Path  string `json:"path"`
	Value string `json:"value"`
}

// OK reports a run with no violations and no error.
func (r Result) OK() bool { return len(r.Violations) == 0 && r.Error == "" }

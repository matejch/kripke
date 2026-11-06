// Package core is the production settlement logic: pure, no I/O, checked by purity and driven by the model.
package core

import (
	"errors"
	"slices"
)

var ErrDuplicate = errors.New("instruction already settled")

type Instruction struct {
	ID     int
	Amount int
}

type Entry struct {
	ID     int
	Amount int
}

type Ledger struct {
	Entries []Entry
}

type Policy struct {
	Idempotent bool
}

// Settle books the instruction and returns the new ledger; the receiver is not modified.
func (l Ledger) Settle(in Instruction, p Policy) (Ledger, error) {
	if p.Idempotent && l.Count(in.ID) > 0 {
		return l, ErrDuplicate
	}
	out := Ledger{Entries: slices.Clone(l.Entries)}
	i, _ := slices.BinarySearchFunc(out.Entries, in.ID, func(e Entry, id int) int { return e.ID - id })
	out.Entries = slices.Insert(out.Entries, i, Entry(in))
	return out, nil
}

// Count is how many entries the ledger holds for an instruction.
func (l Ledger) Count(id int) int {
	n := 0
	for _, e := range l.Entries {
		if e.ID == id {
			n++
		}
	}
	return n
}

// Clone returns an independent copy.
func (l Ledger) Clone() Ledger { return Ledger{Entries: slices.Clone(l.Entries)} }

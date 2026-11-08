// Package core is the post-trade settlement logic: pure, no I/O, checked by purity and driven by the model.
package core

import (
	"errors"
	"slices"
)

var (
	ErrNotClaimed     = errors.New("trade is not claimed by this job")
	ErrAlreadySettled = errors.New("trade already settled")
	ErrUnknownTrade   = errors.New("unknown trade")
)

type Ccy string

type Status int

const (
	Open Status = iota
	Claimed
	Settled
)

func (s Status) String() string { return [...]string{"open", "claimed", "settled"}[s] }

type Trade struct {
	ID        int
	Ccy       Ccy
	Status    Status
	ClaimedBy int
}

type Posting struct {
	Trade int
	Job   int
}

type Kind int

const (
	Batch Kind = iota
	Currency
	EndOfDay
)

func (k Kind) String() string { return [...]string{"batch", "currency", "end-of-day"}[k] }

type Scope struct {
	Kind Kind
	IDs  []int
	Ccy  Ccy
}

type Policy struct {
	Claim       bool
	CheckStatus bool
}

type Book struct {
	Trades   []Trade
	Postings []Posting
}

// Covers reports whether a trade falls inside a request's scope.
func (s Scope) Covers(t Trade) bool {
	switch s.Kind {
	case Batch:
		return slices.Contains(s.IDs, t.ID)
	case Currency:
		return t.Ccy == s.Ccy
	}
	return true
}

// Clone returns an independent copy.
func (b Book) Clone() Book {
	return Book{Trades: slices.Clone(b.Trades), Postings: slices.Clone(b.Postings)}
}

// Select returns the open trades in scope, in ID order.
func (b Book) Select(s Scope) []int {
	var ids []int
	for _, t := range b.Trades {
		if t.Status == Open && s.Covers(t) {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

// Claim moves each listed open trade to claimed-by-job in one transaction and returns the ones it got.
func (b Book) Claim(job int, ids []int) (Book, []int) {
	out := b.Clone()
	var got []int
	for i := range out.Trades {
		t := &out.Trades[i]
		if t.Status == Open && slices.Contains(ids, t.ID) {
			t.Status, t.ClaimedBy = Claimed, job
			got = append(got, t.ID)
		}
	}
	return out, got
}

// Settle marks the trade settled and books one posting for it.
func (b Book) Settle(job, id int, p Policy) (Book, error) {
	i := slices.IndexFunc(b.Trades, func(t Trade) bool { return t.ID == id })
	if i < 0 {
		return b, ErrUnknownTrade
	}
	t := b.Trades[i]
	if p.Claim && (t.Status != Claimed || t.ClaimedBy != job) {
		return b, ErrNotClaimed
	}
	if p.CheckStatus && t.Status == Settled {
		return b, ErrAlreadySettled
	}
	out := b.Clone()
	out.Trades[i].Status, out.Trades[i].ClaimedBy = Settled, 0
	out.Postings = append(out.Postings, Posting{Trade: id, Job: job})
	return out, nil
}

// Release returns every trade claimed by the job to open.
func (b Book) Release(job int) Book {
	out := b.Clone()
	for i := range out.Trades {
		if out.Trades[i].Status == Claimed && out.Trades[i].ClaimedBy == job {
			out.Trades[i].Status, out.Trades[i].ClaimedBy = Open, 0
		}
	}
	return out
}

// Posted counts the postings booked for a trade.
func (b Book) Posted(id int) int {
	n := 0
	for _, p := range b.Postings {
		if p.Trade == id {
			n++
		}
	}
	return n
}

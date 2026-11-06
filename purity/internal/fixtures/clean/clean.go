// Package clean is a pure fixture: arithmetic and a sentinel error.
package clean

import (
	"errors"
	"time"
)

// ErrNegative is returned for negative amounts.
var ErrNegative = errors.New("negative amount")

var _ = Settle

// Settle adds amount to balance and rejects negative amounts.
func Settle(balance, amount int) (int, error) {
	if amount < 0 {
		return balance, ErrNegative
	}
	return balance + amount, nil
}

// Deadline adds a timeout to a logical timestamp.
func Deadline(at, timeout time.Duration) time.Duration {
	return at + timeout
}

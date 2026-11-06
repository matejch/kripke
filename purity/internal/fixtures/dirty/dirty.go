// Package dirty is a fixture with one violation of each purity rule.
package dirty

import (
	"os"
	"time"
)

var counter int

func init() {
	counter = 1
}

// Run trips the go, chan, select, clock and import rules.
func Run() int {
	ch := make(chan int, 1)
	go func() { ch <- 1 }()
	select {
	case v := <-ch:
		counter += v
	default:
	}
	_ = time.Now()
	return counter + len(os.Args)
}

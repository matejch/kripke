package fv

import "fmt"

// Choice records the nondeterministic picks of one Step run so the engine can replay every branch.
type Choice struct {
	prefix []int32
	vals   []int32
	arity  []int32
	forced bool
}

type choiceMismatch struct{ msg string }

// Pick returns a value in [0, n); the engine runs the step once per value.
func (c *Choice) Pick(n int) int {
	if n <= 0 {
		panic(fmt.Sprintf("fv: Pick(%d): n must be positive", n))
	}
	if n == 1 {
		return 0
	}
	pos := len(c.vals)
	v := int32(0)
	if pos < len(c.prefix) {
		v = c.prefix[pos]
		if int(v) >= n {
			panic(choiceMismatch{fmt.Sprintf("choice %d: replayed value %d but Pick(%d) now", pos, v, n)})
		}
	} else if c.forced {
		panic(choiceMismatch{fmt.Sprintf("choice %d: Pick(%d) called beyond the recorded log", pos, n)})
	}
	c.vals = append(c.vals, v)
	c.arity = append(c.arity, int32(n))
	return int(v)
}

// Bool is Pick(2) == 1.
func (c *Choice) Bool() bool { return c.Pick(2) == 1 }

// Any picks one element of xs.
func Any[T any](c *Choice, xs []T) T { return xs[c.Pick(len(xs))] }

// Choices returns the values picked so far in this run.
func (c *Choice) Choices() []int {
	out := make([]int, len(c.vals))
	for i, v := range c.vals {
		out[i] = int(v)
	}
	return out
}

func (c *Choice) next() []int32 {
	for j := len(c.vals) - 1; j >= 0; j-- {
		if c.vals[j]+1 < c.arity[j] {
			p := make([]int32, j+1)
			copy(p, c.vals[:j])
			p[j] = c.vals[j] + 1
			return p
		}
	}
	return nil
}

func (c *Choice) reset(prefix []int32, forced bool) {
	c.prefix = prefix
	c.vals = c.vals[:0]
	c.arity = c.arity[:0]
	c.forced = forced
}

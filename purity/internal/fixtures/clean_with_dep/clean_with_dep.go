// Package clean_with_dep is a pure fixture that imports a pure same-module helper.
package clean_with_dep

import "github.com/matejch/kripke/purity/internal/fixtures/clean_with_dep/mathx"

// Limit clamps v to [0, 100].
func Limit(v int) int {
	return mathx.Clamp(v, 0, 100)
}

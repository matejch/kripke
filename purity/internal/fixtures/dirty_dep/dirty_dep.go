// Package dirty_dep is clean itself but imports an impure same-module helper.
package dirty_dep

import "github.com/matejch/kripke/purity/internal/fixtures/dirty_dep/osutil"

// Count returns the number of process arguments.
func Count() int {
	return len(osutil.Args())
}

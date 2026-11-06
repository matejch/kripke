// Package osutil is an impure same-module helper that imports os.
package osutil

import "os"

// Args returns the process arguments.
func Args() []string {
	return os.Args
}

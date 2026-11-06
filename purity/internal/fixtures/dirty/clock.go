package dirty

import tm "time"

// Elapsed reads the clock through a renamed import.
func Elapsed(start tm.Time) tm.Duration {
	return tm.Since(start)
}

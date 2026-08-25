package domain

// Clock is the injectable logical clock required for deterministic concurrency
// and failure tests. All lease expirations, retry scheduling and audit ordering
// read time exclusively through this interface, never wall-clock time.
type Clock interface {
	// Now returns the current logical time.
	Now() LogicalTime
}

// FixedClock always returns the same logical time. It is the default clock for
// tests and the deterministic server, so scheduling never depends on the wall
// clock or goroutine ordering.
type FixedClock LogicalTime

// Now implements Clock.
func (c FixedClock) Now() LogicalTime { return LogicalTime(c) }

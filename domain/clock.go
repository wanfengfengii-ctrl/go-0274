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

// SteppingClock is a logical clock that a deterministic test can advance by a
// fixed delta. It is the counterpart to FixedClock for flows whose correctness
// depends on the passage of logical time, such as device-call retry backoff:
// the test advances the clock past a scheduled next_retry before re-invoking a
// retry, instead of waiting on the wall clock.
type SteppingClock struct {
	Now_ LogicalTime
	Step LogicalTime
}

// NewSteppingClock builds a stepping clock starting at now and advancing by
// step on every call to Advance.
func NewSteppingClock(now, step LogicalTime) *SteppingClock {
	return &SteppingClock{Now_: now, Step: step}
}

// Now implements Clock.
func (c *SteppingClock) Now() LogicalTime { return c.Now_ }

// Advance moves the clock forward by its configured step and returns the new
// logical time. Call it to reach a scheduled retry time deterministically.
func (c *SteppingClock) Advance() LogicalTime {
	c.Now_ += c.Step
	return c.Now_
}

// SetTime jumps the clock to an explicit logical time, used to land exactly on
// a scheduled next_retry boundary rather than overshooting it.
func (c *SteppingClock) SetTime(t LogicalTime) { c.Now_ = t }

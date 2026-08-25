package adjudication

import (
	"testing"

	"oyster-purification-release-gate/domain"
)

func TestNextRetryAfterIsDeterministic(t *testing.T) {
	now := domain.LogicalTime(100)
	got := []domain.LogicalTime{
		NextRetryAfter(now, 0),
		NextRetryAfter(now, 1),
		NextRetryAfter(now, 2),
		NextRetryAfter(now, 3),
	}
	want := []domain.LogicalTime{101, 102, 104, 108}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attempt %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestNextRetryAfterGuardsOverflow(t *testing.T) {
	// A very large attempt must not overflow the logical clock.
	got := NextRetryAfter(domain.LogicalTime(10), 200)
	if got <= domain.LogicalTime(10) {
		t.Fatalf("retry time should be strictly increasing, got %d", got)
	}
}

package ledger

import "oyster-purification-release-gate/domain"

// ResourceKind classifies a leased resource (purification pool, circulation
// pump branch, probe window, or a toxin/pathogen/culture detection hole).
type ResourceKind string

const (
	ResourcePool  ResourceKind = "pool"
	ResourcePump  ResourceKind = "pump"
	ResourceProbe ResourceKind = "probe"
	ResourceHole  ResourceKind = "hole"
)

// ResourceKey is the globally unique identity of a leased resource. A valid
// lease holds this key exclusively (enforced by a unique constraint).
type ResourceKey string

// Lease is a resource occupation bound to a task generation and a logical
// expiration time. Leases are only released inside the finalizing transaction.
type Lease struct {
	Kind       ResourceKind
	Key        ResourceKey
	TaskID     domain.TaskID
	Generation domain.Generation
	AcquiredAt domain.LogicalTime
	ExpiresAt  domain.LogicalTime
}

// Valid reports whether the lease is currently within its logical window.
func (l Lease) Valid(now domain.LogicalTime) bool {
	return l.AcquiredAt <= now && now < l.ExpiresAt
}

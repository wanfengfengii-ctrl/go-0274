// Package catalog implements the 海区与净化规则目录 component: it maintains
// rule snapshots, validates the area/tide matching relationship and detects
// stale rule digests before a task may lock onto an immutable snapshot.
package catalog

import "oyster-purification-release-gate/domain"

// TidePattern is an allowed tidal batch pattern (for example "T1", "T2").
type TidePattern string

// RuleSnapshot is an immutable snapshot of a purification rule set. Once a task
// locks onto a snapshot the values below never change, satisfying the
// "锁定后任务只引用不可变快照" rule.
type RuleSnapshot struct {
	ID      string
	Version int64
	// Digest is the canonical normalized summary used for staleness detection.
	Digest string
	// AreaID is the fictional harvest area this snapshot applies to.
	AreaID string
	// Tides lists the tidal batch patterns allowed for AreaID.
	Tides []TidePattern
	// Scales holds the fixed-decimal precision for every measurement.
	Scales Scales
	// Thresholds holds the integer thresholds enforced during arbitration.
	Thresholds Thresholds
}

// CheckDigest rejects a submitted rule digest that differs from the snapshot's
// canonical digest, producing STALE_RULE_DIGEST.
func (s RuleSnapshot) CheckDigest(submitted string) error {
	if s.Digest != submitted {
		return &domain.APIError{
			Code:    domain.CodeStaleRuleDigest,
			Message: "rule digest is stale",
		}
	}
	return nil
}

// ValidateAreaTide rejects an area/tide pair that does not belong to this
// snapshot, producing AREA_TIDE_MISMATCH. It returns nil when the area matches
// and the tide is one of the allowed patterns.
func (s RuleSnapshot) ValidateAreaTide(areaID string, tide TidePattern) error {
	if s.AreaID != areaID {
		return &domain.APIError{
			Code:    domain.CodeAreaTideMismatch,
			Message: "harvest area does not match rule snapshot",
		}
	}
	for _, t := range s.Tides {
		if t == tide {
			return nil
		}
	}
	return &domain.APIError{
		Code:    domain.CodeAreaTideMismatch,
		Message: "tidal batch not allowed for area",
	}
}

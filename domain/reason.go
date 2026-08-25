package domain

import "sort"

// ReasonCategory orders failure reasons the way the failure-boundary contract
// requires: by resource category first (area, tide batch, cage seal, timepoint,
// detection hole) and by error code second.
type ReasonCategory int

const (
	ReasonArea ReasonCategory = iota
	ReasonTide
	ReasonSeal
	ReasonTimepoint
	ReasonHole
)

// Reason is a single ordered failure reason attached to an APIError.
type Reason struct {
	Category ReasonCategory
	Code     ErrorCode
	Detail   string
}

func (r Reason) String() string {
	s := string(r.Code)
	if r.Detail != "" {
		s += ": " + r.Detail
	}
	return s
}

// SortReasons orders reasons by category, then by error code. The sort is
// stable so equal (category, code) pairs keep their original insertion order.
func SortReasons(reasons []Reason) {
	sort.SliceStable(reasons, func(i, j int) bool {
		if reasons[i].Category != reasons[j].Category {
			return reasons[i].Category < reasons[j].Category
		}
		return reasons[i].Code < reasons[j].Code
	})
}

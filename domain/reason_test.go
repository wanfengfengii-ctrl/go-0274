package domain

import "testing"

func TestSortReasonsOrdersByCategoryThenCode(t *testing.T) {
	reasons := []Reason{
		{Category: ReasonHole, Code: CodeResourceBusy, Detail: "h1"},
		{Category: ReasonArea, Code: CodeAreaTideMismatch, Detail: "east"},
		{Category: ReasonArea, Code: CodeStaleRuleDigest, Detail: "east"},
		{Category: ReasonSeal, Code: CodeDuplicateSeal, Detail: "s1"},
		{Category: ReasonTimepoint, Code: CodeCoverageInvalid, Detail: "t2"},
	}
	SortReasons(reasons)

	if reasons[0].Category != ReasonArea || reasons[0].Code != CodeAreaTideMismatch {
		t.Fatalf("first reason out of order: %+v", reasons[0])
	}
	if reasons[1].Category != ReasonArea || reasons[1].Code != CodeStaleRuleDigest {
		t.Fatalf("second reason out of order: %+v", reasons[1])
	}
	if reasons[2].Category != ReasonSeal {
		t.Fatalf("third reason out of order: %+v", reasons[2])
	}
	if reasons[3].Category != ReasonTimepoint {
		t.Fatalf("fourth reason out of order: %+v", reasons[3])
	}
	if reasons[4].Category != ReasonHole {
		t.Fatalf("fifth reason out of order: %+v", reasons[4])
	}
}

func TestAPIErrorErrorString(t *testing.T) {
	err := NewError(CodeResourceBusy, "pool occupied")
	if err.Error() != "RESOURCE_BUSY: pool occupied" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
}

func TestWithReasonPreservesOriginal(t *testing.T) {
	base := NewError(CodeCoverageInvalid, "missing cell")
	with := base.WithReason("cage A")
	if len(base.Reasons) != 0 {
		t.Fatalf("WithReason mutated the original error")
	}
	if len(with.Reasons) != 1 || with.Reasons[0] != "cage A" {
		t.Fatalf("unexpected reasons: %v", with.Reasons)
	}
}

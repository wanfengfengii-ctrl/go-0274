package task

import (
	"errors"
	"testing"

	"oyster-purification-release-gate/domain"
)

func TestTerminalStates(t *testing.T) {
	for _, s := range []Status{StatusReleased, StatusIsolated, StatusCancelled} {
		if !s.IsTerminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	for _, s := range []Status{StatusPendingLock, StatusPendingIntake, StatusVitalityCollecting, StatusReleasable} {
		if s.IsTerminal() {
			t.Fatalf("%s should not be terminal", s)
		}
	}
}

func TestValidateGenerationRejectsStale(t *testing.T) {
	err := ValidateGeneration(2, 1)
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeStaleGeneration {
		t.Fatalf("expected STALE_GENERATION, got %v", err)
	}
	if err := ValidateGeneration(2, 2); err != nil {
		t.Fatalf("expected matching generation accepted, got %v", err)
	}
}

func TestCheckOperationIdempotency(t *testing.T) {
	prev := &Receipt{OperationID: "op-1", Digest: "d1", Result: "ok"}

	if _, replay := CheckOperation(prev, "op-1", "d1"); !replay {
		t.Fatalf("same digest should replay")
	}
	if _, replay := CheckOperation(nil, "op-2", "d2"); replay {
		t.Fatalf("nil receipt should not replay")
	}
	err, replay := CheckOperation(prev, "op-1", "d2")
	if replay {
		t.Fatalf("different digest must not replay")
	}
	var apiErr *domain.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != domain.CodeOperationConflict {
		t.Fatalf("expected OPERATION_CONFLICT, got %v", err)
	}
}

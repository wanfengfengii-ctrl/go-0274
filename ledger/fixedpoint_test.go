package ledger

import (
	"errors"
	"testing"
)

func TestParseScaledValid(t *testing.T) {
	v, err := ParseScaled("12.34", 2, false)
	if err != nil || v != 1234 {
		t.Fatalf("got (%d, %v), want (1234, nil)", v, err)
	}
	// Integer input at scale 0.
	v, err = ParseScaled("7", 0, false)
	if err != nil || v != 7 {
		t.Fatalf("got (%d, %v), want (7, nil)", v, err)
	}
	// Trailing zeros pad to scale.
	v, err = ParseScaled("1.5", 3, false)
	if err != nil || v != 1500 {
		t.Fatalf("got (%d, %v), want (1500, nil)", v, err)
	}
}

func TestParseScaledSignHandling(t *testing.T) {
	if _, err := ParseScaled("-3.2", 1, true); err != nil {
		t.Fatalf("signed value should parse: %v", err)
	}
	_, err := ParseScaled("-3.2", 1, false)
	if !errors.Is(err, ErrSign) {
		t.Fatalf("unsigned field should reject sign, got %v", err)
	}
}

func TestParseScaledRejectsTooManyFractionDigits(t *testing.T) {
	if _, err := ParseScaled("1.234", 2, false); !errors.Is(err, ErrSyntax) {
		t.Fatalf("expected ErrSyntax, got %v", err)
	}
}

func TestParseScaledRejectsOverflow(t *testing.T) {
	if _, err := ParseScaled("99999999999999999999", 0, false); !errors.Is(err, ErrOverflow) {
		t.Fatalf("expected ErrOverflow, got %v", err)
	}
}

func TestParseScaledRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", ".", "a1", "1..2", "1.2.3"} {
		if _, err := ParseScaled(in, 2, true); err == nil {
			t.Fatalf("input %q should be rejected", in)
		}
	}
}

func TestCompareScaledIntegerOnly(t *testing.T) {
	if CompareScaled(1500, 1500) != 0 {
		t.Fatalf("equal values should compare 0")
	}
	if CompareScaled(999, 1000) != -1 {
		t.Fatalf("smaller should compare -1")
	}
	if CompareScaled(1001, 1000) != 1 {
		t.Fatalf("larger should compare 1")
	}
}

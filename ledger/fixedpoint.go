// Package ledger implements the 贝笼样本与资源占用账簿 and 活力理化及毒素采集账簿
// components: fixed-decimal integer arithmetic, vitality count conservation and
// the resource-lease model. It never uses floating point.
package ledger

import (
	"errors"
	"math"
	"strings"
)

// Errors returned by fixed-point parsing. They are surfaced as
// FIXED_POINT_OVERFLOW / COVERAGE_INVALID at the business boundary.
var (
	ErrEmpty      = errors.New("empty fixed-point text")
	ErrSyntax     = errors.New("invalid fixed-point text")
	ErrSign       = errors.New("unexpected sign")
	ErrOverflow   = errors.New("fixed-point overflow")
	ErrDivideZero = errors.New("division by zero")
)

// ParseScaled converts a fixed-point decimal text into a scaled integer:
// value * 10^decimals. It enforces the documented textual checks (length, sign,
// overflow) and rejects any input that could not be represented exactly at the
// declared scale. signed controls whether a leading '-' is accepted; when
// false the parsed value is guaranteed non-negative.
func ParseScaled(text string, decimals int, signed bool) (int64, error) {
	if decimals < 0 {
		return 0, ErrSyntax
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, ErrEmpty
	}

	neg := false
	switch {
	case strings.HasPrefix(text, "-"):
		if !signed {
			return 0, ErrSign
		}
		neg = true
		text = text[1:]
	case strings.HasPrefix(text, "+"):
		text = text[1:]
	}
	if text == "" {
		return 0, ErrSyntax
	}

	intPart := text
	fracPart := ""
	if i := strings.IndexByte(text, '.'); i >= 0 {
		intPart, fracPart = text[:i], text[i+1:]
		if strings.Contains(fracPart, ".") {
			return 0, ErrSyntax
		}
	}
	if intPart == "" && fracPart == "" {
		return 0, ErrSyntax
	}
	if len(fracPart) > decimals {
		// More fractional digits than the declared scale cannot be represented
		// exactly; reject rather than silently truncate.
		return 0, ErrSyntax
	}
	fracPart += strings.Repeat("0", decimals-len(fracPart))

	// The scaled value is the concatenation of the integer and zero-padded
	// fractional digits read as a plain integer.
	digits := intPart + fracPart
	var scaled int64
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, ErrSyntax
		}
		d := int64(c - '0')
		if scaled > (math.MaxInt64-d)/10 {
			return 0, ErrOverflow
		}
		scaled = scaled*10 + d
	}
	if neg {
		scaled = -scaled
	}
	return scaled, nil
}

// Scale returns 10^decimals, guarding against unreasonable scales.
func Scale(decimals int) (int64, error) {
	if decimals < 0 {
		return 0, ErrSyntax
	}
	v := int64(1)
	for i := 0; i < decimals; i++ {
		if v > math.MaxInt64/10 {
			return 0, ErrOverflow
		}
		v *= 10
	}
	return v, nil
}

// CompareScaled compares a and b as scaled integers, returning -1, 0 or 1.
// Threshold comparisons use this integer form only.
func CompareScaled(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

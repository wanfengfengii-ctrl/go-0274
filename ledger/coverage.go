package ledger

import (
	"sort"

	"oyster-purification-release-gate/domain"
)

// CoverageCell identifies one (cage, timepoint, observation-point) cell that
// the vitality matrix must fill before the matrix can close.
type CoverageCell struct {
	CageSeal           string
	TimepointID        string
	ObservationPointID string
}

// CoverageMatrix is the full set of cells a locked task requires. Closing the
// matrix means every required cell has exactly one valid vitality record.
type CoverageMatrix struct {
	CageSeals         []string
	Timepoints        []string
	ObservationPoints []string
}

// Required returns the total number of cells in the matrix.
func (m CoverageMatrix) Required() int {
	return len(m.CageSeals) * len(m.Timepoints) * len(m.ObservationPoints)
}

// Missing returns the required cells that are absent from the submitted set,
// sorted deterministically by cage seal, then timepoint, then observation
// point. The deterministic order is what the API returns as its sorted
// missing-coverage list.
func (m CoverageMatrix) Missing(submitted map[CoverageCell]bool) []CoverageCell {
	var missing []CoverageCell
	for _, c := range m.CageSeals {
		for _, t := range m.Timepoints {
			for _, o := range m.ObservationPoints {
				cell := CoverageCell{CageSeal: c, TimepointID: t, ObservationPointID: o}
				if !submitted[cell] {
					missing = append(missing, cell)
				}
			}
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].CageSeal != missing[j].CageSeal {
			return missing[i].CageSeal < missing[j].CageSeal
		}
		if missing[i].TimepointID != missing[j].TimepointID {
			return missing[i].TimepointID < missing[j].TimepointID
		}
		return missing[i].ObservationPointID < missing[j].ObservationPointID
	})
	return missing
}

// Closed reports whether the submitted set covers every required cell.
func (m CoverageMatrix) Closed(submitted map[CoverageCell]bool) bool {
	return len(m.Missing(submitted)) == 0
}

// ErrCoverageInvalid builds a COVERAGE_INVALID error carrying a reason.
func ErrCoverageInvalid(msg string) *domain.APIError {
	return &domain.APIError{Code: domain.CodeCoverageInvalid, Message: msg}
}

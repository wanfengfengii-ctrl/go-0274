package task

import "oyster-purification-release-gate/domain"

// Cage is one oyster cage bound to the task at lock time via its seal.
type Cage struct {
	Seal string
}

// Timepoint is one purification timepoint at which vitality and physchem
// evidence is collected. The locked coverage matrix spans every cage, every
// timepoint and every observation point.
type Timepoint struct {
	ID string
}

// ObservationPoint is one observation point within a cage at a timepoint.
type ObservationPoint struct {
	ID string
}

// Task is the release-task aggregate. It carries the immutable snapshot
// references and the coverage dimensions that every later evidence write must
// satisfy. Mutations are performed only through generation-checked commands.
type Task struct {
	ID                domain.TaskID
	Status            Status
	Generation        domain.Generation
	AreaID            string
	TideBatch         string
	RuleSnapshotID    string
	RuleDigest        string
	LockedCount       int64
	Cages             []Cage
	Timepoints        []Timepoint
	ObservationPoints []ObservationPoint
	IntakeConfirmers  []domain.PersonID
	Reviewers         []domain.PersonID
}

// Terminal reports whether the task has reached one of the three final states.
func (t Task) Terminal() bool { return t.Status.IsTerminal() }

// GenerationMatches is a convenience wrapper over the pure generation check.
func (t Task) GenerationMatches(submitted domain.Generation) error {
	return ValidateGeneration(t.Generation, submitted)
}

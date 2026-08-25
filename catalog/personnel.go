package catalog

import (
	"oyster-purification-release-gate/domain"
)

// Role is a qualification a person holds. The project requires distinct roles
// for intake confirmation and independent review so no single person can both
// confirm a batch and later sign off its release.
type Role string

const (
	RoleIntakeConfirmer Role = "intake_confirmer"
	RoleReviewer        Role = "reviewer"
	RoleLabTechnician   Role = "lab_technician"
)

// Person is an operator with a set of granted roles.
type Person struct {
	ID    domain.PersonID
	Roles []Role
}

// HasRole reports whether the person holds a specific role.
func (p Person) HasRole(r Role) bool {
	for _, got := range p.Roles {
		if got == r {
			return true
		}
	}
	return false
}

// ValidateIntakeConfirmers enforces the two-person intake rule: the two
// confirmers must be distinct and both qualified, otherwise ROLE_CONFLICT.
func ValidateIntakeConfirmers(a, b Person) error {
	if a.ID == b.ID {
		return &domain.APIError{
			Code:    domain.CodeRoleConflict,
			Message: "intake confirmers must be two distinct people",
		}
	}
	for _, p := range []Person{a, b} {
		if !p.HasRole(RoleIntakeConfirmer) {
			return &domain.APIError{
				Code:    domain.CodeRoleConflict,
				Message: "intake confirmer lacks qualification",
			}
		}
	}
	return nil
}

// ValidateReviewers enforces the independent review rule: the two reviewers
// must be distinct, both qualified, and neither may overlap with either intake
// confirmer.
func ValidateReviewers(r1, r2 Person, confirmers ...Person) error {
	if r1.ID == r2.ID {
		return &domain.APIError{
			Code:    domain.CodeRoleConflict,
			Message: "reviewers must be two distinct people",
		}
	}
	for _, p := range []Person{r1, r2} {
		if !p.HasRole(RoleReviewer) {
			return &domain.APIError{
				Code:    domain.CodeRoleConflict,
				Message: "reviewer lacks qualification",
			}
		}
		for _, c := range confirmers {
			if p.ID == c.ID {
				return &domain.APIError{
					Code:    domain.CodeRoleConflict,
					Message: "reviewer overlaps with an intake confirmer",
				}
			}
		}
	}
	return nil
}

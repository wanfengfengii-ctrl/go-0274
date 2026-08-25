package task

import "oyster-purification-release-gate/domain"

// Receipt is a recorded operation result used for idempotent replay. It maps an
// operation id to the normalized request digest and the stable result summary
// that were produced the first time the operation ran.
type Receipt struct {
	OperationID domain.OperationID
	Digest      string
	Result      string
}

// CheckOperation implements the idempotency rule: an operation id paired with
// the same normalized digest replays the previous result, while the same id
// with different content is an OPERATION_CONFLICT. A nil previous receipt means
// this operation has not been seen before.
func CheckOperation(previous *Receipt, op domain.OperationID, digest string) (*domain.APIError, bool) {
	if previous == nil {
		return nil, false
	}
	if previous.Digest == digest {
		return nil, true
	}
	return &domain.APIError{
		Code:    domain.CodeOperationConflict,
		Message: "operation id reused with different content",
	}, false
}

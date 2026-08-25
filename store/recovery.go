package store

import (
	"context"

	"oyster-purification-release-gate/domain"
)

// Recover describes what the recovery scanner found after a restart: the open
// tasks and the failed device calls whose logical retry time has arrived. The
// application uses this to resume exactly where it left off without
// duplicating evidence or re-running finished work.
type Recover struct {
	OpenTasks    []TaskRecord
	PendingCalls []DeviceCallRecord
	ActiveLeases []LeaseRecord
	OpenReceipts int
}

// ScanRecovery reads the persisted state needed to resume after a restart. It
// runs inside a read transaction so the snapshot is consistent.
func (s *Store) ScanRecovery(ctx context.Context, now domain.LogicalTime) (*Recover, error) {
	var out Recover
	err := s.Tx(ctx, func(tx *Tx) error {
		tasks, err := tx.ListOpenTasks(ctx)
		if err != nil {
			return err
		}
		out.OpenTasks = tasks
		calls, err := tx.ListPendingDeviceCalls(ctx, int64(now))
		if err != nil {
			return err
		}
		out.PendingCalls = calls
		var leases []LeaseRecord
		for _, task := range tasks {
			ls, err := tx.ListLeasesByTask(ctx, task.ID)
			if err != nil {
				return err
			}
			leases = append(leases, ls...)
		}
		out.ActiveLeases = leases
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

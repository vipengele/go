package selfupdate

import (
	"context"
	"fmt"

	"go.vipengele.org/daemon"
)

// ReleaseInfo is the release channel and watermark a daemon follows, as the consumer's update
// source knows them. Both are plain strings, copied into the reported daemon.UpdateStatus.
type ReleaseInfo struct {
	Channel   string
	Watermark string
}

// Reporter returns the daemon.UpdateReporter that reports the update recorded in store. It only
// calls Load, so it writes nothing and takes no lock.
//
// A missing record is daemon.UpdateIdle. A record that cannot be read, a done ctx, or any other
// store error is daemon.UpdateUnknown with the cause in Error: an unreadable record is never
// reported as idle. The record's paths are not reported.
//
// release supplies the Channel and Watermark and may be nil, which reports neither. When release
// fails, the status read from the record stands and Error carries the failure.
func Reporter(store Store, release func(ctx context.Context) (ReleaseInfo, error)) daemon.UpdateReporter {
	return func(ctx context.Context) daemon.UpdateStatus {
		if err := ctx.Err(); err != nil {
			return unreadable(err)
		}

		rec, err := store.Load(ctx)
		if err != nil {
			return unreadable(err)
		}

		st := daemon.UpdateStatus{
			State:        updateState(rec.Status),
			FromVersion:  rec.FromVersion,
			ToVersion:    rec.ToVersion,
			BootAttempts: rec.BootAttempts,
			Reason:       rec.Reason,
			StartedAt:    rec.StartedAt,
			UpdatedAt:    rec.UpdatedAt,
		}

		if release == nil {
			return st
		}

		info, err := release(ctx)
		if err != nil {
			st.Error = fmt.Sprintf("selfupdate: release info: %v", err)

			return st
		}

		st.Channel = info.Channel
		st.Watermark = info.Watermark

		return st
	}
}

// unreadable is the status of a record that could not be read.
func unreadable(err error) daemon.UpdateStatus {
	return daemon.UpdateStatus{State: daemon.UpdateUnknown, Error: err.Error()}
}

// updateState maps a Status to its daemon.UpdateState. A Status outside the four, which a decoded
// record never holds, is daemon.UpdateUnknown.
func updateState(s Status) daemon.UpdateState {
	switch s {
	case StatusIdle:
		return daemon.UpdateIdle
	case StatusPending:
		return daemon.UpdatePending
	case StatusConfirmed:
		return daemon.UpdateConfirmed
	case StatusRolledBack:
		return daemon.UpdateRolledBack
	default:
		return daemon.UpdateUnknown
	}
}

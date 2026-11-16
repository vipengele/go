package daemoncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/table"
	"go.vipengele.org/daemon"
)

// updateStatusSchema is the version of the JSON shape update-status writes: source, the keys of
// [daemon.UpdateStatus] and daemon_error. It is versioned apart from the health envelope's schema.
const updateStatusSchema = 1

// The sources update-status reads the update status from, as its "source" key names them.
const (
	// sourceDaemon is the "update" object of the running daemon's /status.
	sourceDaemon = "daemon"
	// sourceStore is the fallback [daemon.UpdateReporter], which reads the state the self-update
	// keeps on disk.
	sourceStore = "store"
	// sourceNone is a reachable daemon that reports no update, with no fallback to ask.
	sourceNone = "none"
)

// ErrUpdateNotOK reports an update status that is rolled_back or unknown, or that carries an
// error. update-status wraps it in [spec.Reported], since the status it has printed already says
// so.
var ErrUpdateNotOK = errors.New("self-update is not ok")

// NewUpdateStatus returns the "update-status" command, which prints where the daemon's
// self-update stands. It asks the daemon for its snapshot at "/status" below the base URL resolve
// returns, as [NewStatus] does, and prints the snapshot's update. It panics when resolve is nil.
//
// fallback is asked instead whenever the daemon gives no update: a daemon the command cannot
// resolve or reach, a request that times out, an answer other than 200, a body it cannot read, or
// a snapshot without an update. A nil fallback means there is none. A cancelled context is not a
// failure of the daemon: the command returns its error and does not ask fallback.
//
// The command prints a table of field and value or, when [spec.Output.Format] is
// [spec.FormatJSON], one object with [spec.WriteJSON] at schema 1: source, which is daemon, store
// or none, the keys of [daemon.UpdateStatus], and daemon_error, why the daemon was not used, when
// a failure of the daemon made the command ask fallback. Such a failure is also noted on Stderr as
// a warning, which --quiet drops.
//
// It returns nil for an update that is idle, pending or confirmed, and for a reachable daemon that
// reports no update when there is no fallback. An update that is rolled_back or unknown, or that
// carries an error, is printed all the same, and the command returns [ErrUpdateNotOK] wrapped in
// [spec.Reported]. A daemon that fails with no fallback to ask is an ordinary error, and the
// command prints nothing.
func NewUpdateStatus(resolve Resolver, fallback daemon.UpdateReporter, opts ...StatusOption) *spec.Command {
	if resolve == nil {
		panic("daemoncmd: NewUpdateStatus: resolve is nil")
	}
	s := statusSettings{client: http.DefaultClient, timeout: defaultTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}
	return &spec.Command{
		Name:    "update-status",
		Summary: "Print the state of the daemon's self-update",
		Description: "Print where the daemon's self-update stands, as the running daemon reports it, " +
			"or from the self-update's own state when the daemon cannot say. Exits 1 while the last " +
			"update is rolled back or unknown, or carries an error.",
		Args: spec.NoArgs(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			report, err := readUpdate(ctx, inv, s, resolve, fallback)
			if err != nil {
				return fmt.Errorf("update-status: %w", err)
			}
			if report.DaemonError != "" {
				inv.Warning("daemon not used (%s); read the update status from the store", report.DaemonError)
			}
			if inv.Output.Format() == spec.FormatJSON {
				err = spec.WriteJSON(inv.Env.Stdout, updateStatusSchema, report)
			} else {
				err = renderUpdate(inv.Env.Stdout, inv.Output.Styler(spec.Stdout), report)
			}
			if err != nil {
				return err
			}
			if u := report.Update; u != nil && !updateOK(*u) {
				if u.Error != "" {
					return spec.Reported(fmt.Errorf("%w: %s: %s", ErrUpdateNotOK, u.State, u.Error))
				}
				return spec.Reported(fmt.Errorf("%w: %s", ErrUpdateNotOK, u.State))
			}
			return nil
		},
	}
}

// readUpdate returns the update status from the daemon, or from fallback when the daemon gives
// none. It returns an error when the context is done, or when the daemon fails and fallback is
// nil.
func readUpdate(ctx context.Context, inv *spec.Invocation, s statusSettings, resolve Resolver,
	fallback daemon.UpdateReporter,
) (updateReport, error) {
	update, daemonErr := askDaemon(ctx, inv, s, resolve)
	if daemonErr != nil {
		if err := ctx.Err(); err != nil {
			return updateReport{}, daemonErr
		}
	}
	if update != nil {
		return updateReport{Source: sourceDaemon, Update: update}, nil
	}
	if fallback == nil {
		if daemonErr != nil {
			return updateReport{}, daemonErr
		}
		return updateReport{Source: sourceNone}, nil
	}
	u := fallback(ctx)
	report := updateReport{Source: sourceStore, Update: &u}
	if daemonErr != nil {
		report.DaemonError = daemonErr.Error()
	}
	return report, nil
}

// askDaemon returns the update of the daemon's snapshot: nil with a nil error for a daemon that
// reports none, and an error when the daemon cannot be resolved, reached or read.
func askDaemon(ctx context.Context, inv *spec.Invocation, s statusSettings, resolve Resolver) (*daemon.UpdateStatus, error) {
	base, err := resolve(ctx, inv)
	if err != nil {
		return nil, fmt.Errorf("resolve the daemon's URL: %w", err)
	}
	snap, err := fetch(ctx, s, base)
	if err != nil {
		return nil, err
	}
	return snap.Update, nil
}

// updateOK reports whether u is idle, pending or confirmed, without an error.
func updateOK(u daemon.UpdateStatus) bool {
	switch u.State {
	case daemon.UpdateIdle, daemon.UpdatePending, daemon.UpdateConfirmed:
		return u.Error == ""
	default:
		return false
	}
}

// updateReport is what update-status prints: where it read the update from, the update, nil for
// sourceNone, and, when a failure of the daemon made it read the store, that failure.
type updateReport struct {
	Source      string
	Update      *daemon.UpdateStatus
	DaemonError string
}

// MarshalJSON encodes r as one flat object: source, then the keys of Update as
// [daemon.UpdateStatus.MarshalJSON] writes them, none when Update is nil, then daemon_error,
// omitted when empty.
func (r updateReport) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(nestedReport(r))
	return liftUpdate(data, r.DaemonError != ""), err
}

// nestedReport is an updateReport, field for field, that encodes its update as an object of its
// own, which liftUpdate flattens into the report.
type nestedReport struct {
	Source      string               `json:"source"`
	Update      *daemon.UpdateStatus `json:"update,omitempty"`
	DaemonError string               `json:"daemon_error,omitempty"`
}

// liftUpdate replaces the "update" member of data, a compact nestedReport, with the keys of its
// object, and returns data unchanged when it has none. hasDaemonError is whether data carries a
// daemon_error member. Every quote inside a JSON string is escaped, so the first `,"update":{` is
// the key and the last `,"daemon_error":` the key after the update's object.
func liftUpdate(data []byte, hasDaemonError bool) []byte {
	head, rest, found := bytes.Cut(data, []byte(`,"update":{`))
	if !found {
		return data
	}
	// rest is the update's members, the closing brace of its object, then the member after it:
	// daemon_error, or the report's closing brace.
	body, tail := rest[:len(rest)-2], rest[len(rest)-1:]
	if hasDaemonError {
		end := bytes.LastIndex(rest, []byte(`,"daemon_error":`))
		body, tail = rest[:end-1], rest[end:]
	}
	out := make([]byte, 0, len(data))
	out = append(out, head...)
	out = append(out, ',')
	out = append(out, body...)
	return append(out, tail...)
}

// renderUpdate writes r as a two-column table of field and value: the source and the update's
// status, then each other field of the update that is set. With no update it writes one line
// saying self-update is not configured.
func renderUpdate(w io.Writer, st spec.Styler, r updateReport) error {
	if r.Update == nil {
		if _, err := io.WriteString(w, "self-update is not configured\n"); err != nil {
			return fmt.Errorf("update-status: write: %w", err)
		}
		return nil
	}
	u := *r.Update
	t := table.New(
		table.Column{Key: "field", Header: "FIELD"},
		table.Column{Key: "value", Header: "VALUE"},
	)
	t.Add("source", r.Source)
	t.Add("status", u.State)
	addSet := func(field, value string) {
		if value != "" {
			t.Add(field, value)
		}
	}
	addSet("from version", u.FromVersion)
	addSet("to version", u.ToVersion)
	if u.BootAttempts != 0 {
		t.Add("boot attempts", u.BootAttempts)
	}
	addSet("reason", u.Reason)
	addSet("started at", formatTime(u.StartedAt))
	addSet("updated at", formatTime(u.UpdatedAt))
	addSet("channel", u.Channel)
	addSet("watermark", u.Watermark)
	addSet("error", u.Error)
	return t.Render(w, st)
}

// formatTime is t in RFC 3339 in UTC to the second, as the JSON writes it, or empty when t is zero.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

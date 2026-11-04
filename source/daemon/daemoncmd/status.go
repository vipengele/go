package daemoncmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/table"
	"go.vipengele.org/daemon"
)

// schema is the version of the JSON shape the command writes and reads: the envelope that
// (*daemon.Health).Handler serves at /status.
const schema = 1

// defaultTimeout bounds one request for the status when WithTimeout is not given.
const defaultTimeout = 5 * time.Second

// maxBody is the largest /status body the command reads.
const maxBody = 1 << 20

// ErrNotOK reports a daemon whose snapshot is unready or down. The command wraps it in
// [spec.Reported], since the snapshot it has printed already says so.
var ErrNotOK = errors.New("daemon is not ok")

// ErrBadStatus reports a /status response the command cannot read as a snapshot: an answer
// other than 200, a body that is not the envelope at schema 1, or an unknown overall status.
var ErrBadStatus = errors.New("bad status response")

// Resolver returns the base URL the daemon serves its health routes under, such as
// "http://127.0.0.1:8080" or "http://127.0.0.1:8080/health". The command requests "/status"
// below it.
type Resolver func(ctx context.Context, inv *spec.Invocation) (string, error)

// StatusOption configures the command [NewStatus] returns.
type StatusOption func(*statusSettings)

// statusSettings holds what the [StatusOption]s given to [NewStatus] set.
type statusSettings struct {
	client  *http.Client
	timeout time.Duration
}

// WithHTTPClient makes the command request the status with client. The default is
// [http.DefaultClient]. A nil client leaves the default in place.
func WithHTTPClient(client *http.Client) StatusOption {
	return func(s *statusSettings) {
		if client != nil {
			s.client = client
		}
	}
}

// WithTimeout bounds one request for the status, reading its body included. The default is five
// seconds. A timeout that is zero or negative leaves the default in place.
func WithTimeout(timeout time.Duration) StatusOption {
	return func(s *statusSettings) {
		if timeout > 0 {
			s.timeout = timeout
		}
	}
}

// NewStatus returns the "status" command, which asks a running daemon for its health snapshot at
// "/status" below the base URL resolve returns, and prints it. It panics when resolve is nil.
//
// The command prints the snapshot as text tables, or, when [spec.Output.Format] is
// [spec.FormatJSON], as the JSON envelope the daemon served, with [spec.WriteJSON] at schema 1.
// It declares no --json flag of its own. It returns nil when the snapshot is ok. When it is
// unready or down, it prints the snapshot all the same and returns [ErrNotOK] wrapped in
// [spec.Reported]. A daemon it cannot reach, an answer other than 200, and a body it cannot
// read are ordinary errors, and it prints nothing.
func NewStatus(resolve Resolver, opts ...StatusOption) *spec.Command {
	if resolve == nil {
		panic("daemoncmd: NewStatus: resolve is nil")
	}
	s := statusSettings{client: http.DefaultClient, timeout: defaultTimeout}
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}
	return &spec.Command{
		Name:    "status",
		Summary: "Print the health of the running daemon",
		Description: "Print the health snapshot of the running daemon: its overall status and the " +
			"result of each health check. Exits 0 only when the daemon is ok.",
		Args: spec.NoArgs(),
		Run: func(ctx context.Context, inv *spec.Invocation) error {
			base, err := resolve(ctx, inv)
			if err != nil {
				return fmt.Errorf("status: resolve the daemon's URL: %w", err)
			}
			snap, err := fetch(ctx, s, base)
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}
			if inv.Output.Format() == spec.FormatJSON {
				err = spec.WriteJSON(inv.Env.Stdout, schema, snap)
			} else {
				err = render(inv.Env.Stdout, inv.Output.Styler(spec.Stdout), snap)
			}
			if err != nil {
				return err
			}
			if snap.Status != daemon.StateOK {
				return spec.Reported(fmt.Errorf("%w: %s", ErrNotOK, snap.Status))
			}
			return nil
		},
	}
}

// fetch requests the status at "/status" below base and decodes it.
func fetch(ctx context.Context, s statusSettings, base string) (daemon.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, http.NoBody)
	if err != nil {
		return daemon.Snapshot{}, fmt.Errorf("daemon URL %q: %w", base, err)
	}
	// A base with no path joins as a relative path, which no server accepts as a request target.
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}
	req.URL = req.URL.JoinPath("status")
	target := req.URL.String()
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return daemon.Snapshot{}, fmt.Errorf("reach the daemon: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return daemon.Snapshot{}, fmt.Errorf("%w: GET %s answered %s", ErrBadStatus, target, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return daemon.Snapshot{}, fmt.Errorf("read the status from %s: %w", target, err)
	}
	if len(body) > maxBody {
		return daemon.Snapshot{}, fmt.Errorf("%w: GET %s answered more than %d bytes", ErrBadStatus, target, maxBody)
	}
	return decode(body)
}

// decode reads body as the /status envelope at schema 1.
func decode(body []byte) (daemon.Snapshot, error) {
	var envelope struct {
		Schema *int `json:"schema"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return daemon.Snapshot{}, fmt.Errorf("%w: %w", ErrBadStatus, err)
	}
	switch {
	case envelope.Schema == nil:
		return daemon.Snapshot{}, fmt.Errorf("%w: no schema", ErrBadStatus)
	case *envelope.Schema != schema:
		return daemon.Snapshot{}, fmt.Errorf("%w: schema %d, want %d", ErrBadStatus, *envelope.Schema, schema)
	}
	var snap daemon.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return daemon.Snapshot{}, fmt.Errorf("%w: %w", ErrBadStatus, err)
	}
	switch snap.Status {
	case daemon.StateOK, daemon.StateUnready, daemon.StateDown:
	default:
		return daemon.Snapshot{}, fmt.Errorf("%w: unknown status %q", ErrBadStatus, snap.Status)
	}
	return snap, nil
}

// render writes snap as two tables separated by a blank line: the overall status, then one row
// per check.
func render(w io.Writer, st spec.Styler, snap daemon.Snapshot) error {
	overall := table.New(
		table.Column{Key: "status", Header: "STATUS", Style: styleState},
		table.Column{Key: "live", Header: "LIVE"},
		table.Column{Key: "ready", Header: "READY"},
		table.Column{Key: "uptime", Header: "UPTIME"},
	)
	overall.Add(snap.Status, snap.Live, snap.Ready, snap.Uptime)

	checks := table.New(
		table.Column{Key: "name", Header: "CHECK"},
		table.Column{Key: "kinds", Header: "KINDS"},
		table.Column{Key: "status", Header: "STATUS", Style: styleCheck},
		table.Column{Key: "duration", Header: "DURATION", Right: true},
		table.Column{Key: "error", Header: "ERROR"},
	)
	for _, c := range snap.Checks {
		kinds := make([]string, len(c.Kinds))
		for i, k := range c.Kinds {
			kinds[i] = k.String()
		}
		checks.Add(c.Name, strings.Join(kinds, ","), c.Status, c.Duration, c.Error)
	}

	if err := overall.Render(w, st); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("status: write: %w", err)
	}
	return checks.Render(w, st)
}

// styleState marks an overall status: ok as a success, unready as a warning, down as an error.
func styleState(st spec.Styler, text string) string {
	switch daemon.State(text) {
	case daemon.StateOK:
		return st.Success(text)
	case daemon.StateUnready:
		return st.Warning(text)
	default:
		return st.Error(text)
	}
}

// styleCheck marks a check's status: pass as a success, fail as an error.
func styleCheck(st spec.Styler, text string) string {
	if daemon.CheckStatus(text) == daemon.CheckPass {
		return st.Success(text)
	}
	return st.Error(text)
}

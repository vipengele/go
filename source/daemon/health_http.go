package daemon

import (
	"bytes"
	"net/http"
	"strconv"

	"go.vipengele.org/cli/spec"
)

// statusSchema is the schema version of the /status envelope.
const statusSchema = 1

// Handler returns the http.Handler that answers the health routes. Each request runs the checks
// afresh under the request's context.
//
//   - /livez runs only the Liveness checks, a check of both kinds included, and answers 200 with
//     "ok" when they all pass and 503 with "down" otherwise. A Readiness check never runs for it,
//     so a slow or failing dependency neither delays nor fails the probe that decides a restart.
//   - /readyz runs every check and answers 200 when the snapshot is ready and 503 otherwise, with
//     its overall status: "ok", "unready" or "down".
//   - Both write their status and a newline as text/plain.
//   - /status always answers 200 with the snapshot as an application/json envelope whose first key
//     is "schema".
//
// Any other path answers 404 and any method but GET and HEAD answers 405 with an Allow header. A
// HEAD request gets the headers of the GET response and no body. Every response carries
// "Cache-Control: no-store". The handler mounts no prefix: a consumer that serves it under one
// strips the prefix first, and decides who may reach it.
func (h *Health) Handler() http.Handler {
	return http.HandlerFunc(h.serveHTTP)
}

func (h *Health) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var route func(Snapshot) (int, string)
	var only Kind
	switch r.URL.Path {
	case "/livez":
		only = Liveness
		route = func(s Snapshot) (int, string) { return probeCode(s.Live), string(s.Status) + "\n" }
	case "/readyz":
		route = func(s Snapshot) (int, string) { return probeCode(s.Ready), string(s.Status) + "\n" }
	case "/status":
	default:
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	snap := h.snapshot(r.Context(), only)
	code, contentType := http.StatusOK, "application/json"
	var body bytes.Buffer
	if route != nil {
		var text string
		code, text = route(snap)
		contentType = "text/plain; charset=utf-8"
		body.WriteString(text)
	} else if err := spec.WriteJSON(&body, statusSchema, snap); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(body.Len()))
	w.WriteHeader(code)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}

// probeCode is 200 for a probe that holds and 503 for one that does not.
func probeCode(ok bool) int {
	if ok {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}

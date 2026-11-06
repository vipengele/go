package httpserver_test

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.vipengele.org/daemon/httpserver"
)

const plainHTTPWarning = "exposed over plain HTTP beyond loopback"

func mustNew(t *testing.T, handler http.Handler, opts ...httpserver.Option) *http.Server {
	t.Helper()
	srv, err := httpserver.New(handler, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

// localRequest returns a request whose Host AllowHosts lets through.
func localRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "127.0.0.1"
	return r
}

func TestNewDefaults(t *testing.T) {
	srv := mustNew(t, okHandler(), httpserver.WithAddr("127.0.0.1:0"))

	if srv.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q, want 127.0.0.1:0", srv.Addr)
	}
	for _, c := range []struct {
		name      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 30 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 30 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 120 * time.Second},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if srv.MaxHeaderBytes != 64<<10 {
		t.Errorf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, 64<<10)
	}
	if srv.TLSConfig != nil {
		t.Errorf("TLSConfig = %v, want nil", srv.TLSConfig)
	}
	if srv.ErrorLog == nil {
		t.Error("ErrorLog is nil, want the logger's")
	}
}

func TestNewLeavesBaseContextToTheRunner(t *testing.T) {
	srv := mustNew(t, okHandler())
	if srv.BaseContext != nil {
		t.Error("BaseContext is set, want nil")
	}
}

func TestNewRejectsANilHandler(t *testing.T) {
	srv, err := httpserver.New(nil)
	if !errors.Is(err, httpserver.ErrNilHandler) {
		t.Errorf("err = %v, want ErrNilHandler", err)
	}
	if srv != nil {
		t.Errorf("srv = %v, want nil", srv)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	for name, opt := range map[string]httpserver.Option{
		"nil logger":            httpserver.WithLogger(nil),
		"nil tls config":        httpserver.WithTLS(nil),
		"nil authenticator":     httpserver.WithAuthenticator(nil),
		"nil token func":        httpserver.WithBearerTokenFunc(nil),
		"negative read header":  httpserver.WithTimeouts(httpserver.Timeouts{ReadHeader: -time.Second}),
		"negative read":         httpserver.WithTimeouts(httpserver.Timeouts{Read: -time.Second}),
		"negative write":        httpserver.WithTimeouts(httpserver.Timeouts{Write: -time.Second}),
		"negative idle":         httpserver.WithTimeouts(httpserver.Timeouts{Idle: -time.Second}),
		"zero max body bytes":   httpserver.WithMaxBodyBytes(0),
		"negative max body len": httpserver.WithMaxBodyBytes(-1),
	} {
		t.Run(name, func(t *testing.T) {
			srv, err := httpserver.New(okHandler(), opt)
			if err == nil {
				t.Fatal("New succeeded, want an error")
			}
			if srv != nil {
				t.Errorf("srv = %v, want nil", srv)
			}
		})
	}
}

func TestNewSkipsANilOption(t *testing.T) {
	mustNew(t, okHandler(), nil)
}

func TestWithTimeoutsOverridesOnlyNonZeroFields(t *testing.T) {
	srv := mustNew(t, okHandler(), httpserver.WithTimeouts(httpserver.Timeouts{
		ReadHeader: time.Second,
		Idle:       time.Minute,
	}))

	for _, c := range []struct {
		name      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, time.Second},
		{"ReadTimeout", srv.ReadTimeout, httpserver.DefaultReadTimeout},
		{"WriteTimeout", srv.WriteTimeout, httpserver.DefaultWriteTimeout},
		{"IdleTimeout", srv.IdleTimeout, time.Minute},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	srv = mustNew(t, okHandler(), httpserver.WithTimeouts(httpserver.Timeouts{
		Read:  2 * time.Second,
		Write: 3 * time.Second,
	}))
	if srv.ReadTimeout != 2*time.Second || srv.WriteTimeout != 3*time.Second {
		t.Errorf("Read, Write = %v, %v, want 2s, 3s", srv.ReadTimeout, srv.WriteTimeout)
	}
	if srv.ReadHeaderTimeout != httpserver.DefaultReadHeaderTimeout || srv.IdleTimeout != httpserver.DefaultIdleTimeout {
		t.Errorf("ReadHeader, Idle = %v, %v, want the defaults", srv.ReadHeaderTimeout, srv.IdleTimeout)
	}
}

func decodeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]string
		if err := httpserver.DecodeJSON(r, &v); err != nil {
			var re *httpserver.RequestError
			if errors.As(err, &re) {
				httpserver.Error(w, re.Status, re.Code, re.Message)
				return
			}
			httpserver.Error(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func jsonBody(size int) string {
	return `{"k":"` + strings.Repeat("a", size) + `"}`
}

func postJSON(srv *http.Server, body string) *httptest.ResponseRecorder {
	r := localRequest(http.MethodPost, "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, r)
	return rec
}

func TestNewLimitsTheBodyToTheDefault(t *testing.T) {
	srv := mustNew(t, decodeHandler())

	if rec := postJSON(srv, jsonBody(httpserver.DefaultMaxBodyBytes-100)); rec.Code != http.StatusNoContent {
		t.Errorf("body under the limit: status = %d, want 204", rec.Code)
	}
	rec := postJSON(srv, jsonBody(httpserver.DefaultMaxBodyBytes))
	assertEnvelope(t, rec, http.StatusRequestEntityTooLarge, "body_too_large")
}

func TestWithMaxBodyBytesSetsTheBodyLimit(t *testing.T) {
	srv := mustNew(t, decodeHandler(), httpserver.WithMaxBodyBytes(64))

	if rec := postJSON(srv, jsonBody(10)); rec.Code != http.StatusNoContent {
		t.Errorf("body under the limit: status = %d, want 204", rec.Code)
	}
	rec := postJSON(srv, jsonBody(100))
	assertEnvelope(t, rec, http.StatusRequestEntityTooLarge, "body_too_large")
}

func TestWithTLSClonesAndRaisesMinVersion(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS10, ServerName: "example"} //nolint:gosec // the minimum New raises
	srv := mustNew(t, okHandler(), httpserver.WithTLS(cfg))

	if srv.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if srv.TLSConfig == cfg {
		t.Error("TLSConfig is the caller's config, want a clone")
	}
	if srv.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %#x, want TLS 1.2", srv.TLSConfig.MinVersion)
	}
	if srv.TLSConfig.ServerName != "example" {
		t.Errorf("ServerName = %q, want the caller's", srv.TLSConfig.ServerName)
	}
	if cfg.MinVersion != tls.VersionTLS10 {
		t.Errorf("caller's MinVersion = %#x, want it unchanged", cfg.MinVersion)
	}
}

func TestWithTLSRaisesAnUnsetMinVersion(t *testing.T) {
	srv := mustNew(t, okHandler(), httpserver.WithTLS(&tls.Config{})) //nolint:gosec // the minimum New raises
	if srv.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %#x, want TLS 1.2", srv.TLSConfig.MinVersion)
	}
}

func TestWithTLSKeepsAHigherMinVersion(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS13}
	srv := mustNew(t, okHandler(), httpserver.WithTLS(cfg))
	if srv.TLSConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %#x, want TLS 1.3", srv.TLSConfig.MinVersion)
	}
}

func TestNewWarnsAboutPlainHTTPBeyondLoopback(t *testing.T) {
	for _, addr := range []string{"", ":8080", "0.0.0.0:8080", "[::]:8080", "192.168.1.10:8080", "example.com:8080"} {
		t.Run(addr, func(t *testing.T) {
			logger, logs := newLogger()
			mustNew(t, okHandler(), httpserver.WithLogger(logger), httpserver.WithAddr(addr))

			out := logs.String()
			if !strings.Contains(out, "level=WARN") || !strings.Contains(out, plainHTTPWarning) {
				t.Errorf("log %q lacks the plain HTTP warning", out)
			}
			if !strings.Contains(out, "addr="+addr) {
				t.Errorf("log %q does not name the address %q", out, addr)
			}
		})
	}
}

func TestNewIsSilentOnLoopbackOrTLS(t *testing.T) {
	for name, opts := range map[string][]httpserver.Option{
		"127.0.0.1":      {httpserver.WithAddr("127.0.0.1:8080")},
		"127.0.0.2":      {httpserver.WithAddr("127.0.0.2:8080")},
		"::1":            {httpserver.WithAddr("[::1]:8080")},
		"localhost":      {httpserver.WithAddr("localhost:8080")},
		"LocalHost":      {httpserver.WithAddr("LocalHost:8080")},
		"tls on 0.0.0.0": {httpserver.WithAddr("0.0.0.0:8443"), httpserver.WithTLS(&tls.Config{MinVersion: tls.VersionTLS12})},
		"tls on empty":   {httpserver.WithTLS(&tls.Config{MinVersion: tls.VersionTLS12})},
	} {
		t.Run(name, func(t *testing.T) {
			logger, logs := newLogger()
			mustNew(t, okHandler(), append(opts, httpserver.WithLogger(logger))...)
			if out := logs.String(); out != "" {
				t.Errorf("log = %q, want nothing", out)
			}
		})
	}
}

// fakeListener is a net.Listener that is never bound and reports addr as its address.
type fakeListener struct {
	addr net.Addr
}

func (fakeListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (fakeListener) Close() error              { return nil }
func (l fakeListener) Addr() net.Addr          { return l.addr }

func TestCheckListenerIsSilentOnLoopback(t *testing.T) {
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	logger, logs := newLogger()
	srv := mustNew(t, okHandler(), httpserver.WithAddr("127.0.0.1:0"))

	httpserver.CheckListener(srv, l, logger)

	if out := logs.String(); out != "" {
		t.Errorf("log = %q, want nothing", out)
	}
}

func TestCheckListenerWarnsAboutPlainHTTPOnAllInterfaces(t *testing.T) {
	l := fakeListener{addr: &net.TCPAddr{IP: net.IPv4zero, Port: 8080}}
	logger, logs := newLogger()
	srv := mustNew(t, okHandler(), httpserver.WithAddr("127.0.0.1:0"))

	httpserver.CheckListener(srv, l, logger)

	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, plainHTTPWarning) {
		t.Errorf("log %q lacks the plain HTTP warning", out)
	}
	if !strings.Contains(out, "addr=0.0.0.0:8080") {
		t.Errorf("log %q does not name the bound address", out)
	}
}

func TestCheckListenerIsSilentForTLS(t *testing.T) {
	l := fakeListener{addr: &net.TCPAddr{IP: net.IPv4zero, Port: 8443}}
	logger, logs := newLogger()
	srv := mustNew(t, okHandler(), httpserver.WithTLS(&tls.Config{MinVersion: tls.VersionTLS12}))

	httpserver.CheckListener(srv, l, logger)

	if out := logs.String(); out != "" {
		t.Errorf("log = %q, want nothing", out)
	}
}

func TestCheckListenerIsSilentForAUnixSocket(t *testing.T) {
	l := fakeListener{addr: &net.UnixAddr{Name: "/run/app.sock", Net: "unix"}}
	logger, logs := newLogger()

	httpserver.CheckListener(&http.Server{}, l, logger)

	if out := logs.String(); out != "" {
		t.Errorf("log = %q, want nothing", out)
	}
}

func TestNewLogsAndSecuresAHostRejection(t *testing.T) {
	logger, logs := newLogger()
	srv := mustNew(t, okHandler(), httpserver.WithLogger(logger), httpserver.WithAddr("127.0.0.1:0"))
	r := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	r.Host = "evil.example"
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, r)

	assertEnvelope(t, rec, http.StatusMisdirectedRequest, "misdirected_request")
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	out := logs.String()
	for _, want := range []string{"http request", "path=/jobs", "status=421"} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q lacks %q", out, want)
		}
	}
}

func TestNewAllowsConfiguredHostsAndOrigins(t *testing.T) {
	srv := mustNew(t, okHandler(),
		httpserver.WithAllowedHosts("app.example"),
		httpserver.WithAllowedOrigins("https://app.example"),
	)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "app.example"
	r.Header.Set("Origin", "https://app.example")
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Errorf("allowed host and origin: status = %d, want 200", rec.Code)
	}

	r = localRequest(http.MethodGet, "/", nil)
	r.Header.Set("Origin", "https://other.example")
	rec = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, r)
	assertEnvelope(t, rec, http.StatusForbidden, "forbidden_origin")
}

func TestNewRecoversAHandlerPanic(t *testing.T) {
	logger, logs := newLogger()
	srv := mustNew(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), httpserver.WithLogger(logger), httpserver.WithAddr("127.0.0.1:0"))
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, localRequest(http.MethodGet, "/", nil))

	assertEnvelope(t, rec, http.StatusInternalServerError, "internal_error")
	if out := logs.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "http handler panic") {
		t.Errorf("log %q lacks the panic", out)
	}
}

func TestNewAuthenticatesAllButTheUnauthenticatedPaths(t *testing.T) {
	srv := mustNew(t, okHandler(),
		httpserver.WithBearerTokenFunc(func() string { return "secret" }),
		httpserver.WithUnauthenticatedPaths("/livez"),
	)
	serve := func(path, authorization string) *httptest.ResponseRecorder {
		r := localRequest(http.MethodGet, path, nil)
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, r)
		return rec
	}

	if rec := serve("/livez", ""); rec.Code != http.StatusOK {
		t.Errorf("/livez without a token: status = %d, want 200", rec.Code)
	}
	rec := serve("/jobs", "")
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	assertEnvelope(t, serve("/jobs", "Bearer wrong"), http.StatusUnauthorized, "unauthorized")
	if rec := serve("/jobs", "Bearer secret"); rec.Code != http.StatusOK {
		t.Errorf("/jobs with the token: status = %d, want 200", rec.Code)
	}
}

func TestNewWithAuthenticatorUsesIt(t *testing.T) {
	srv := mustNew(t, okHandler(), httpserver.WithAuthenticator(httpserver.BearerToken(func() string { return "t" })))
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, localRequest(http.MethodGet, "/", nil))
	assertEnvelope(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestNewWithoutAuthenticatorServesEveryPath(t *testing.T) {
	srv := mustNew(t, okHandler())
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, localRequest(http.MethodGet, "/jobs", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestCheckListenerWithANilLoggerWarnsOnTheDefaultLogger(t *testing.T) {
	logs := setDefaultLogger(t)
	l := fakeListener{addr: &net.TCPAddr{IP: net.IPv4zero, Port: 8080}}
	srv := mustNew(t, okHandler(), httpserver.WithAddr("127.0.0.1:0"))

	httpserver.CheckListener(srv, l, nil)

	if out := logs.String(); !strings.Contains(out, plainHTTPWarning) {
		t.Errorf("default log = %q lacks the plain HTTP warning", out)
	}
}

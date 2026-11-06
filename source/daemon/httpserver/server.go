package httpserver

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// The defaults of the server New builds.
const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 30 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultMaxHeaderBytes    = 64 << 10
	DefaultMaxBodyBytes      = 1 << 20
)

// ErrNilHandler is what New returns for a nil handler.
var ErrNilHandler = errors.New("httpserver: nil handler")

// New returns an *http.Server that serves handler behind the middleware of this package, outermost
// first: Recover, Log, SecureHeaders, AllowHosts, AllowOrigins, RequireAuth when an authenticator
// is configured, and LimitBody. Every middleware and the server's ErrorLog, at Warn level, log to
// the logger WithLogger sets.
//
// Without options the server has ReadHeaderTimeout DefaultReadHeaderTimeout, ReadTimeout
// DefaultReadTimeout, WriteTimeout DefaultWriteTimeout, IdleTimeout DefaultIdleTimeout,
// MaxHeaderBytes DefaultMaxHeaderBytes and a request body limit of DefaultMaxBodyBytes, serves
// plain HTTP and authenticates nothing. BaseContext is left nil: the daemon runner sets it.
//
// Without WithTLS, New logs a Warn when the address of WithAddr is reachable beyond loopback, as
// CheckListener describes. New returns ErrNilHandler for a nil handler, and the error of the first
// Option that rejects its argument; a nil Option is skipped.
func New(handler http.Handler, opts ...Option) (*http.Server, error) {
	if handler == nil {
		return nil, ErrNilHandler
	}
	c := config{
		logger: slog.New(slog.DiscardHandler),
		timeouts: Timeouts{
			ReadHeader: DefaultReadHeaderTimeout,
			Read:       DefaultReadTimeout,
			Write:      DefaultWriteTimeout,
			Idle:       DefaultIdleTimeout,
		},
		maxBodyBytes:   DefaultMaxBodyBytes,
		maxHeaderBytes: DefaultMaxHeaderBytes,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&c); err != nil {
			return nil, err
		}
	}

	h := LimitBody(c.maxBodyBytes)(handler)
	if c.auth != nil {
		h = RequireAuth(c.auth, c.openPaths...)(h)
	}
	h = AllowOrigins(c.origins...)(h)
	h = AllowHosts(c.hosts...)(h)
	h = SecureHeaders()(h)
	h = Log(c.logger)(h)
	h = Recover(c.logger)(h)

	if c.tls == nil && exposedBeyondLoopback(c.addr) {
		warnPlainHTTP(c.logger, c.addr)
	}

	return &http.Server{
		Addr:              c.addr,
		Handler:           h,
		TLSConfig:         c.tls,
		ReadHeaderTimeout: c.timeouts.ReadHeader,
		ReadTimeout:       c.timeouts.Read,
		WriteTimeout:      c.timeouts.Write,
		IdleTimeout:       c.timeouts.Idle,
		MaxHeaderBytes:    c.maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(c.logger.Handler(), slog.LevelWarn),
	}, nil
}

// CheckListener logs a Warn to logger when srv serves plain HTTP on l and the address l is bound
// to is reachable beyond loopback: its host is empty, unspecified (0.0.0.0, ::), an IP address
// that is not loopback, or a name other than "localhost". It is silent when srv has a TLSConfig,
// and for a listener whose network is not TCP, such as a Unix socket. Call it once l is bound, as
// the bound address is the one New could only infer from srv.Addr. A nil logger logs to
// slog.Default(); a nil srv counts as plain HTTP, and a nil l is not checked.
func CheckListener(srv *http.Server, l net.Listener, logger *slog.Logger) {
	if l == nil || (srv != nil && srv.TLSConfig != nil) {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	addr := l.Addr()
	if addr == nil || !strings.HasPrefix(addr.Network(), "tcp") {
		return
	}
	if exposedBeyondLoopback(addr.String()) {
		warnPlainHTTP(logger, addr.String())
	}
}

func warnPlainHTTP(logger *slog.Logger, addr string) {
	logger.Warn("http server is exposed over plain HTTP beyond loopback", slog.String("addr", addr))
}

// exposedBeyondLoopback reports whether a server listening on addr, a "host:port" or a bare host,
// may be reached from beyond the loopback interface. Only "localhost", matched case-insensitively,
// and a loopback IP address are not.
func exposedBeyondLoopback(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return true
	}
	return !ip.Unmap().IsLoopback()
}

package httpserver

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Option configures the server New builds. An Option that rejects its argument returns an error,
// which New reports.
type Option func(*config) error

// Timeouts are the timeouts of the server New builds, each set on the http.Server field of the
// same name: ReadHeader on ReadHeaderTimeout, Read on ReadTimeout, Write on WriteTimeout and Idle
// on IdleTimeout. A zero field keeps the default.
type Timeouts struct {
	ReadHeader time.Duration
	Read       time.Duration
	Write      time.Duration
	Idle       time.Duration
}

// config is what the Options of New set.
type config struct {
	logger         *slog.Logger
	addr           string
	tls            *tls.Config
	hosts          []string
	origins        []string
	auth           Authenticator
	openPaths      []string
	timeouts       Timeouts
	maxBodyBytes   int64
	maxHeaderBytes int
}

// WithLogger sets the logger of the server and of every middleware New installs. Without it they
// log nowhere. A nil logger is rejected by New.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) error {
		if logger == nil {
			return errors.New("httpserver: nil logger")
		}
		c.logger = logger
		return nil
	}
}

// WithAddr sets the address the server listens on, the http.Server Addr. Without it the address
// is empty, which the daemon runner binds as ":http", every interface.
func WithAddr(addr string) Option {
	return func(c *config) error {
		c.addr = addr
		return nil
	}
}

// WithTLS serves the server over TLS with a clone of cfg, its MinVersion raised to TLS 1.2 when
// lower; cfg itself is never changed. The clone is taken when New runs. A nil cfg is rejected by
// New.
func WithTLS(cfg *tls.Config) Option {
	return func(c *config) error {
		if cfg == nil {
			return errors.New("httpserver: nil tls config")
		}
		clone := cfg.Clone()
		clone.MinVersion = max(clone.MinVersion, tls.VersionTLS12)
		c.tls = clone
		return nil
	}
}

// WithAllowedHosts adds hosts to those AllowHosts lets through, beyond localhost and IP literals.
// It may be given more than once; the lists add up.
func WithAllowedHosts(hosts ...string) Option {
	return func(c *config) error {
		c.hosts = append(c.hosts, hosts...)
		return nil
	}
}

// WithAllowedOrigins adds origins to those AllowOrigins lets through. Without it, every request
// that carries an Origin header is refused. It may be given more than once; the lists add up.
func WithAllowedOrigins(origins ...string) Option {
	return func(c *config) error {
		c.origins = append(c.origins, origins...)
		return nil
	}
}

// WithAuthenticator has the server answer 401 to every request a rejects, except on the paths
// WithUnauthenticatedPaths lists. A later WithAuthenticator or WithBearerTokenFunc replaces it. A
// nil a is rejected by New.
func WithAuthenticator(a Authenticator) Option {
	return func(c *config) error {
		if a == nil {
			return errors.New("httpserver: nil authenticator")
		}
		c.auth = a
		return nil
	}
}

// WithBearerTokenFunc is WithAuthenticator(BearerToken(token)). A nil token is rejected by New.
func WithBearerTokenFunc(token func() string) Option {
	return func(c *config) error {
		if token == nil {
			return errors.New("httpserver: nil bearer token func")
		}
		c.auth = BearerToken(token)
		return nil
	}
}

// WithUnauthenticatedPaths adds paths the authenticator skips, matched as RequireAuth matches
// them: only a request whose path is canonical, with no escaping such as "%2F" and already
// cleaned, matches, so "/x/../livez" needs credentials where "/livez" does not. They have no effect without WithAuthenticator or WithBearerTokenFunc, as then no path is
// authenticated. It may be given more than once; the lists add up.
func WithUnauthenticatedPaths(paths ...string) Option {
	return func(c *config) error {
		c.openPaths = append(c.openPaths, paths...)
		return nil
	}
}

// WithTimeouts overrides the server's timeouts, each non-zero field of t replacing its default. A
// negative field is rejected by New.
func WithTimeouts(t Timeouts) Option {
	return func(c *config) error {
		for _, f := range []struct {
			name string
			d    time.Duration
			dst  *time.Duration
		}{
			{"read header", t.ReadHeader, &c.timeouts.ReadHeader},
			{"read", t.Read, &c.timeouts.Read},
			{"write", t.Write, &c.timeouts.Write},
			{"idle", t.Idle, &c.timeouts.Idle},
		} {
			if f.d < 0 {
				return fmt.Errorf("httpserver: %s timeout must not be negative, got %v", f.name, f.d)
			}
			if f.d > 0 {
				*f.dst = f.d
			}
		}
		return nil
	}
}

// WithMaxBodyBytes sets the most bytes of a request body a handler may read; a read past it fails
// as LimitBody describes. A limit that is zero or negative is rejected by New.
func WithMaxBodyBytes(n int64) Option {
	return func(c *config) error {
		if n <= 0 {
			return fmt.Errorf("httpserver: max body bytes must be positive, got %d", n)
		}
		c.maxBodyBytes = n
		return nil
	}
}

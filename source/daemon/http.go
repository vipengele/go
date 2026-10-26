package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// DefaultHTTPOrder is the Order of a Runner built by NewHTTPServer or NewHTTPServerListener
// without WithOrder. It is above the default Order of 0, so the server starts after the Runners
// it depends on and is stopped before them: it stops accepting requests while the work its
// handlers call is still running.
const DefaultHTTPOrder = 100

// defaultHTTPName is the Name of an HTTP Runner built without WithHTTPName.
const defaultHTTPName = "http"

// ErrNilServer is what Start returns on an HTTP Runner built with a nil *http.Server.
var ErrNilServer = errors.New("daemon: nil http server")

// ErrNilListener is what Start returns on a Runner built by NewHTTPServerListener with a nil
// listener.
var ErrNilListener = errors.New("daemon: nil listener")

// errHTTPStarted is what a second Start returns on an HTTP Runner.
var errHTTPStarted = errors.New("daemon: http runner already started")

// HTTPOption configures a Runner built by NewHTTPServer or NewHTTPServerListener.
type HTTPOption interface {
	applyHTTP(r *httpRunner)
}

func (o OrderOption) applyHTTP(r *httpRunner) { r.order = o.order }

// httpNameOption is the HTTPOption WithHTTPName builds.
type httpNameOption string

func (o httpNameOption) applyHTTP(r *httpRunner) { r.name = string(o) }

// WithHTTPName sets the Name of an HTTP Runner, which is "http" without it. It is not validated:
// New rejects a blank name, as it does for any Runner.
func WithHTTPName(name string) HTTPOption {
	return httpNameOption(name)
}

// NewHTTPServer returns a Runner that serves srv on srv.Addr, which Start binds. A failure to
// bind is a start failure, and the Runner is ready once the address is bound. An empty srv.Addr
// is ":http", as for http.Server.ListenAndServe.
//
// See NewHTTPServerListener for how the Runner serves and stops.
func NewHTTPServer(srv *http.Server, opts ...HTTPOption) Runner {
	return newHTTPRunner(srv, nil, false, opts)
}

// NewHTTPServerListener returns a Runner that serves srv on l, a listener the caller has already
// bound. Start does not bind: the Runner is ready as soon as it starts serving.
//
// The Runner is called "http" unless WithHTTPName names it, and has Order DefaultHTTPOrder unless
// WithOrder places it, so by default it starts last and is stopped first.
//
// Start sets srv.BaseContext, replacing any function already there, to return a context that
// carries the values of the Start context but not its cancellation or deadline. Every request
// context derives from it, so handlers keep running when the start phase ends. That context is
// cancelled only once the drain ends.
//
// Stop calls srv.Shutdown with its context: the server stops accepting, and requests in flight
// have until that context is done to finish. When it is done first, Stop calls srv.Close, which
// closes every connection, cancels the base context so handlers that watch their request context
// return, and returns the context's error, which the Supervisor reports as ErrStopTimeout. Done
// closes once Serve has returned and the drain has ended, cut short or not.
//
// After Stop has begun, Serve returning http.ErrServerClosed is a clean exit and Err reports nil.
// Any other return from Serve, http.ErrServerClosed included when something other than Stop shut
// the server down, is the Runner's exit and Err reports it. A panic in Serve is recovered into a
// *PanicError, which becomes the Runner's Err.
//
// Serve takes l over and closes it when the server stops; a listener Start never served stays the
// caller's to close. A nil srv makes Start return ErrNilServer, and a nil l ErrNilListener.
//
// Start may be called once: a second Start returns an error and changes nothing. Stop before Start
// returns nil and leaves the Runner never started: Done never closes, and a Start that follows
// neither binds nor serves and returns an error wrapping context.Canceled. The Supervisor stops a
// Runner whose Start is still pending when the start phase ends, so that Start, losing the race
// to Stop, reports the cancellation it was given rather than a failure.
func NewHTTPServerListener(srv *http.Server, l net.Listener, opts ...HTTPOption) Runner {
	return newHTTPRunner(srv, l, true, opts)
}

func newHTTPRunner(srv *http.Server, l net.Listener, supplied bool, opts []HTTPOption) *httpRunner {
	r := &httpRunner{
		name:        defaultHTTPName,
		order:       DefaultHTTPOrder,
		srv:         srv,
		listener:    l,
		useListener: supplied,
		done:        make(chan struct{}),
		drained:     make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt.applyHTTP(r)
		}
	}
	return r
}

// httpRunner is the Runner NewHTTPServer and NewHTTPServerListener build.
type httpRunner struct {
	name  string
	order int
	srv   *http.Server

	// useListener is set when the caller supplied listener; otherwise Start binds srv.Addr into
	// listener.
	useListener bool

	// done closes once Serve has returned and, when Stop has begun, the drain has ended; err is
	// written before it closes and read only after.
	done chan struct{}
	err  error

	// drained closes once Stop's drain has ended, after the base context is cancelled.
	drained chan struct{}

	mu sync.Mutex
	// listener is what Serve serves, set by Start when it binds.
	listener net.Listener
	// claimed is set by the first Start; only a Start that claims the Runner serves.
	claimed bool
	// stopped is set by Stop; a first Start after it neither binds nor serves.
	stopped bool
	// started is set once Start has launched Serve.
	started bool
	// stopping is set by the first Stop of a started Runner, before it shuts the server down.
	stopping bool
	// cancelBase cancels the context srv.BaseContext returns; it is nil until Start serves.
	cancelBase context.CancelFunc
}

// Name returns the name set by WithHTTPName, or "http".
func (r *httpRunner) Name() string { return r.name }

// Order returns the Order set by WithOrder, or DefaultHTTPOrder.
func (r *httpRunner) Order() int { return r.order }

// Start binds the listener when the Runner has none, launches Serve in a goroutine and returns
// nil. It returns an error when the server or listener is nil, when binding fails, or when the
// Runner was already started, and one wrapping context.Canceled when Stop came first.
func (r *httpRunner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.claimed {
		return fmt.Errorf("daemon: runner %q: %w", r.name, errHTTPStarted)
	}
	r.claimed = true
	if r.stopped {
		return stoppedBeforeStart(r.name)
	}

	if r.srv == nil {
		return fmt.Errorf("daemon: runner %q: %w", r.name, ErrNilServer)
	}
	if r.useListener && r.listener == nil {
		return fmt.Errorf("daemon: runner %q: %w", r.name, ErrNilListener)
	}
	if !r.useListener {
		addr := r.srv.Addr
		if addr == "" {
			addr = ":http"
		}
		var lc net.ListenConfig
		l, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("daemon: runner %q: %w", r.name, err)
		}
		r.listener = l
	}

	base, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancelBase = cancel
	r.srv.BaseContext = func(net.Listener) context.Context { return base }
	r.started = true

	srv, l := r.srv, r.listener
	go func() {
		o := guard(r.name, func() error { return srv.Serve(l) })
		r.mu.Lock()
		stopping := r.stopping
		r.mu.Unlock()
		err := o.err
		if stopping {
			<-r.drained
			if !o.panicked && errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
		}
		r.err = err
		close(r.done)
	}()
	return nil
}

// Done is closed once Serve has returned and any drain Stop began has ended.
func (r *httpRunner) Done() <-chan struct{} { return r.done }

// Err returns why the Runner exited once Done is closed, and nil before.
func (r *httpRunner) Err() error {
	select {
	case <-r.done:
		return r.err
	default:
		return nil
	}
}

// Stop shuts the server down within ctx, closes it when ctx is done first, then cancels the base
// context and waits for Serve to return. It returns ctx's error when the drain was cut short or
// Serve has not returned when ctx is done. A second Stop waits for the first one's drain.
func (r *httpRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.stopped = true
	started, already := r.started, r.stopping
	r.stopping = started
	r.mu.Unlock()
	if !started {
		return nil
	}
	if already {
		return r.wait(ctx)
	}

	err := r.srv.Shutdown(ctx)
	if err != nil {
		err = errors.Join(err, r.srv.Close())
	}
	r.cancelBase()
	close(r.drained)
	if err != nil {
		return err
	}
	return r.wait(ctx)
}

// wait waits for Done to close, returning ctx's error when ctx is done first.
func (r *httpRunner) wait(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	default:
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

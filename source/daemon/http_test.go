package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// httpRun runs a Supervisor over runners in the background, with a stop context the test cancels
// to end the grace period.
type httpRun struct {
	cancelRun  context.CancelFunc
	cancelStop context.CancelFunc
	waiting    chan struct{}
	result     chan error
}

func startHTTPRun(t *testing.T, runners ...Runner) *httpRun {
	t.Helper()
	s, err := New(runners)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := &httpRun{waiting: make(chan struct{}, 16), result: make(chan error, 1)}
	stopCtx, cancelStop := context.WithCancel(context.Background())
	s.newStopContext = func() (context.Context, context.CancelFunc) { return stopCtx, cancelStop }
	s.waiting = func() {
		select {
		case h.waiting <- struct{}{}:
		default:
		}
	}
	runCtx, cancelRun := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "run"))
	h.cancelRun, h.cancelStop = cancelRun, cancelStop
	go func() { h.result <- s.Run(runCtx) }()
	t.Cleanup(cancelRun)
	t.Cleanup(cancelStop)
	return h
}

// listen binds an ephemeral port on the loopback interface.
func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// blockingServer is an *http.Server whose handler reports each request on entered, with the
// request's context, and then waits for release before answering "ok", ignoring its context.
// shutdown closes once Shutdown has begun, and connCtx receives the context of each connection,
// which derives from the server's base context.
type blockingServer struct {
	srv      *http.Server
	entered  chan context.Context
	release  chan struct{}
	shutdown chan struct{}
	connCtx  chan context.Context
}

func newBlockingServer() *blockingServer {
	b := &blockingServer{
		entered:  make(chan context.Context, 1),
		release:  make(chan struct{}),
		shutdown: make(chan struct{}),
		connCtx:  make(chan context.Context, 1),
	}
	b.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			b.entered <- req.Context()
			<-b.release
			_, _ = io.WriteString(w, "ok")
		}),
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			b.connCtx <- ctx
			return ctx
		},
	}
	b.srv.RegisterOnShutdown(func() { close(b.shutdown) })
	return b
}

// response is what a client request came to.
type response struct {
	body string
	err  error
}

// get requests addr in the background.
func get(t *testing.T, addr string) <-chan response {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	out := make(chan response, 1)
	go func() {
		resp, err := client.Get("http://" + addr + "/")
		if err != nil {
			out <- response{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		out <- response{body: string(body), err: err}
	}()
	return out
}

func isDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func TestHTTPInFlightRequestFinishesDuringTheDrain(t *testing.T) {
	b := newBlockingServer()
	l := listen(t)
	r := NewHTTPServerListener(b.srv, l)
	run := startHTTPRun(t, r)
	<-run.waiting

	resp := get(t, l.Addr().String())
	reqCtx := <-b.entered
	connCtx := <-b.connCtx
	if got := reqCtx.Value(ctxKey{}); got != "run" {
		t.Errorf("request context value = %v, want the run context's value", got)
	}

	run.cancelRun()
	<-b.shutdown
	if isDone(connCtx) || isDone(reqCtx) {
		t.Error("base context cancelled while the drain is in progress")
	}
	select {
	case <-r.Done():
		t.Error("Done closed while a request is in flight")
	default:
	}

	close(b.release)
	if got := <-resp; got.err != nil || got.body != "ok" {
		t.Errorf("response = %q, %v, want \"ok\"", got.body, got.err)
	}
	if err := <-run.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
	<-r.Done()
	if !isDone(connCtx) {
		t.Error("base context still live once the drain has ended")
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err = %v, want nil after Stop", err)
	}
}

func TestHTTPHandlerIgnoringItsContextIsClosedAtTheGrace(t *testing.T) {
	b := newBlockingServer()
	t.Cleanup(func() { close(b.release) })
	l := listen(t)
	r := NewHTTPServerListener(b.srv, l)
	run := startHTTPRun(t, r)
	<-run.waiting

	resp := get(t, l.Addr().String())
	reqCtx := <-b.entered
	run.cancelRun()
	<-b.shutdown
	if isDone(reqCtx) {
		t.Error("request context cancelled before the grace period ended")
	}
	run.cancelStop()

	err := <-run.result
	if !errors.Is(err, ErrStopTimeout) || !strings.Contains(err.Error(), `"http"`) {
		t.Errorf("Run = %v, want ErrStopTimeout naming http", err)
	}
	if got := <-resp; got.err == nil {
		t.Errorf("response = %q, want the connection closed", got.body)
	}
	<-reqCtx.Done()
	<-r.Done()
	if err := r.Err(); err != nil {
		t.Errorf("Err = %v, want nil after Stop", err)
	}
}

func TestHTTPStopReturnsTheContextErrorWhenTheDrainIsCutShort(t *testing.T) {
	b := newBlockingServer()
	t.Cleanup(func() { close(b.release) })
	l := listen(t)
	r := NewHTTPServerListener(b.srv, l)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = get(t, l.Addr().String())
	<-b.entered

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop(ctx) }()
	<-b.shutdown
	cancel()
	if err := <-stopped; !errors.Is(err, context.Canceled) {
		t.Errorf("Stop = %v, want context.Canceled", err)
	}
	<-r.Done()
}

func TestHTTPBindFailureIsAStartFailure(t *testing.T) {
	taken := listen(t)
	srv := &http.Server{Addr: taken.Addr().String(), Handler: http.NotFoundHandler()}
	run := startHTTPRun(t, Func("worker", untilStopped), NewHTTPServer(srv))

	err := <-run.result
	if err == nil || !strings.Contains(err.Error(), `"http"`) {
		t.Errorf("Run = %v, want a start failure naming http", err)
	}
	if errors.Is(err, ErrStopTimeout) {
		t.Errorf("Run = %v, want no stop problem", err)
	}
}

func TestHTTPServerBindsItsAddress(t *testing.T) {
	srv := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
	}
	r := NewHTTPServer(srv)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := <-get(t, r.(*httpRunner).listener.Addr().String()); got.err != nil || got.body != "ok" {
		t.Errorf("response = %q, %v, want \"ok\"", got.body, got.err)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Errorf("Stop = %v, want nil", err)
	}
	<-r.Done()
	if err := r.Err(); err != nil {
		t.Errorf("Err = %v, want nil after Stop", err)
	}
}

func TestHTTPServerListenerServesTheCallersListener(t *testing.T) {
	l := listen(t)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }),
	}
	r := NewHTTPServerListener(srv, l)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := <-get(t, l.Addr().String()); got.err != nil || got.body != "ok" {
		t.Errorf("response = %q, %v, want \"ok\"", got.body, got.err)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Errorf("Stop = %v, want nil", err)
	}
	<-r.Done()
	if err := r.Err(); err != nil {
		t.Errorf("Err = %v, want nil after Stop", err)
	}
}

func TestHTTPServerClosedWithoutStopIsAnExit(t *testing.T) {
	srv := &http.Server{Handler: http.NotFoundHandler()}
	r := NewHTTPServerListener(srv, listen(t))
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-r.Done()
	if err := r.Err(); !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Err = %v, want http.ErrServerClosed", err)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Errorf("Stop = %v, want nil", err)
	}
}

func TestHTTPStartRejectsNilServerListenerAndASecondStart(t *testing.T) {
	ctx := context.Background()
	if err := NewHTTPServer(nil).Start(ctx); !errors.Is(err, ErrNilServer) {
		t.Errorf("Start with nil server = %v, want ErrNilServer", err)
	}
	if err := NewHTTPServerListener(nil, listen(t)).Start(ctx); !errors.Is(err, ErrNilServer) {
		t.Errorf("Start with nil server = %v, want ErrNilServer", err)
	}
	srv := &http.Server{Handler: http.NotFoundHandler()}
	if err := NewHTTPServerListener(srv, nil).Start(ctx); !errors.Is(err, ErrNilListener) {
		t.Errorf("Start with nil listener = %v, want ErrNilListener", err)
	}

	r := NewHTTPServerListener(srv, listen(t))
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := r.Start(ctx); err == nil {
		t.Error("second Start = nil, want an error")
	}
	if err := r.Stop(ctx); err != nil {
		t.Errorf("Stop = %v, want nil", err)
	}
	<-r.Done()

	unstarted := NewHTTPServerListener(&http.Server{}, listen(t))
	if err := unstarted.Stop(ctx); err != nil {
		t.Errorf("Stop before Start = %v, want nil", err)
	}
	if err := unstarted.Start(ctx); err == nil {
		t.Error("Start after Stop = nil, want an error")
	}
}

func TestHTTPNameAndOrderOptions(t *testing.T) {
	r := NewHTTPServer(&http.Server{})
	if r.Name() != "http" || orderOf(r) != DefaultHTTPOrder {
		t.Errorf("defaults = %q, %d, want \"http\", %d", r.Name(), orderOf(r), DefaultHTTPOrder)
	}
	r = NewHTTPServer(&http.Server{}, WithHTTPName("api"), WithOrder(-1), nil)
	if r.Name() != "api" || orderOf(r) != -1 {
		t.Errorf("options = %q, %d, want \"api\", -1", r.Name(), orderOf(r))
	}
}

// stopRecorder records the Runner's name when its Stop is called. It keeps the Runner's Order.
type stopRecorder struct {
	Runner
	record func(name string)
}

func (s *stopRecorder) Order() int { return orderOf(s.Runner) }

func (s *stopRecorder) Stop(ctx context.Context) error {
	s.record(s.Name())
	return s.Runner.Stop(ctx)
}

func TestHTTPStopsFirstByDefault(t *testing.T) {
	var mu sync.Mutex
	var stops []string
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		stops = append(stops, name)
	}

	srv := &http.Server{Handler: http.NotFoundHandler()}
	server := &stopRecorder{Runner: NewHTTPServerListener(srv, listen(t)), record: record}
	worker := Func("worker", func(ctx context.Context) error {
		<-ctx.Done()
		record("worker")
		return ctx.Err()
	})
	run := startHTTPRun(t, server, worker)
	<-run.waiting
	run.cancelRun()
	if err := <-run.result; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}

	mu.Lock()
	defer mu.Unlock()
	checkOrder(t, "stop", stops, []string{"http", "worker"})
}

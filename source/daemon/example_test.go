package daemon_test

import (
	"context"
	"fmt"

	"go.vipengele.org/daemon"
)

// worker is a Runner whose work runs in a goroutine until Stop asks it to end. It reports on
// started once it is running.
type worker struct {
	name    string
	started chan<- struct{}
	quit    chan struct{}
	done    chan struct{}
}

func newWorker(name string, started chan<- struct{}) *worker {
	return &worker{name: name, started: started, quit: make(chan struct{}), done: make(chan struct{})}
}

func (w *worker) Name() string { return w.name }

func (w *worker) Start(context.Context) error {
	go func() {
		defer close(w.done)
		<-w.quit
	}()
	fmt.Println("started", w.name)
	w.started <- struct{}{}
	return nil
}

func (w *worker) Done() <-chan struct{} { return w.done }

func (w *worker) Err() error { return nil }

func (w *worker) Stop(context.Context) error {
	fmt.Println("stopping", w.name)
	close(w.quit)
	return nil
}

func ExampleSupervisor() {
	started := make(chan struct{}, 2)
	sup, err := daemon.New([]daemon.Runner{
		newWorker("database", started),
		newWorker("server", started),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	// A daemon cancels this context from its signal handler; here it is cancelled once both
	// Runners are running.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		<-started
		cancel()
	}()

	fmt.Println("run:", sup.Run(ctx))
	// Output:
	// started database
	// started server
	// stopping server
	// stopping database
	// run: <nil>
}

package daemon_test

import (
	"context"
	"fmt"

	"go.vipengele.org/daemon"
)

// stopper is a Runner that reports on started once it is running and prints a line when it is
// stopped.
type stopper struct {
	name    string
	started chan<- struct{}
}

func (s stopper) Name() string { return s.name }

func (s stopper) Start(ctx context.Context) error {
	s.started <- struct{}{}
	<-ctx.Done()
	return nil
}

func (s stopper) Stop(context.Context) error {
	fmt.Println("stopped", s.name)
	return nil
}

func ExampleSupervisor() {
	started := make(chan struct{}, 2)
	sup, err := daemon.New([]daemon.Runner{
		stopper{name: "database", started: started},
		stopper{name: "server", started: started},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		<-started
		cancel()
	}()

	fmt.Println("run:", sup.Run(ctx))
	// Output:
	// stopped server
	// stopped database
	// run: <nil>
}

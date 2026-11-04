package service_test

import (
	"errors"
	"fmt"

	"go.vipengele.org/daemon/service"
)

func ExampleNew() {
	cfg := service.Config{
		Name:        "my-svc",
		Description: "My service",
		Binary:      "/usr/local/bin/svc",
		Args:        []string{"--port", "8080"},
	}
	m, err := service.New(cfg, service.WithGOOS("plan9"))
	if errors.Is(err, service.ErrUnsupported) {
		fmt.Println("no driver:", err)
		return
	}
	fmt.Println(m != nil)
	// Output: no driver: service: unsupported GOOS: plan9
}

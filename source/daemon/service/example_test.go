package service_test

import (
	"errors"
	"fmt"
	"time"

	"go.vipengele.org/daemon/service"
)

func ExampleNew() {
	cfg := service.Config{
		Name:        "my-svc",
		Description: "My service",
		Binary:      "/usr/local/bin/svc",
		Args:        []string{"--port", "8080"},
	}
	m, err := service.New(cfg, service.WithGOOS("plan9"),
		service.WithContainer(func() bool { return false }))
	if errors.Is(err, service.ErrUnsupported) {
		fmt.Println("no driver:", err)
		return
	}
	fmt.Println(m != nil)
	// Output: no driver: service: unsupported GOOS: plan9
}

func ExampleNew_container() {
	cfg := service.Config{Name: "my-svc", Binary: "/usr/local/bin/svc"}
	_, err := service.New(cfg, service.WithGOOS("linux"),
		service.WithContainer(func() bool { return true }))
	var ce *service.ContainerError
	if errors.As(err, &ce) {
		fmt.Println(errors.Is(err, service.ErrContainer))
	}
	// Output: true
}

func ExampleWithSystemdNotify() {
	cfg := service.Config{Name: "my-svc", Binary: "/usr/local/bin/svc"}
	_, err := service.New(cfg, service.WithGOOS("linux"),
		service.WithContainer(func() bool { return false }),
		service.WithSystemdNotify(1500*time.Microsecond))
	fmt.Println(errors.Is(err, service.ErrInvalidConfig))
	// Output: true
}

package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.vipengele.org/config"
)

type serverConfig struct {
	Host       string
	MaxRetries int
}

type appConfig struct {
	Name    string
	Timeout time.Duration
	Server  serverConfig
}

func Example() {
	dir, err := os.MkdirTemp("", "config-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "config.yaml")
	file := "name: demo\nserver.host: example.org\nserver:\n  max-retries: 5\n"
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		fmt.Println(err)
		return
	}

	environ := map[string]string{"APP_TIMEOUT": "90s", "APP_SERVER_MAX_RETRIES": "7"}
	lookup := func(name string) (string, bool) {
		v, ok := environ[name]
		return v, ok
	}

	defaults := appConfig{Name: "app", Timeout: 30 * time.Second, Server: serverConfig{Host: "localhost", MaxRetries: 3}}
	res, err := config.Load(defaults, config.File(path), config.Env("APP", lookup))
	if err != nil {
		fmt.Println(err)
		return
	}

	cfg := res.Value()
	fmt.Println(cfg.Name, cfg.Timeout, cfg.Server.Host, cfg.Server.MaxRetries)
	for _, s := range res.Sources() {
		fmt.Printf("%s = %s (%s)\n", s.Key, s.Value, s.Origin)
	}

	// Output:
	// demo 1m30s example.org 7
	// name = demo (config.yaml:1)
	// timeout = 1m30s (env APP_TIMEOUT)
	// server.host = example.org (config.yaml:2)
	// server.max_retries = 7 (env APP_SERVER_MAX_RETRIES)
}

package config_test

import (
	"errors"
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

type limitsConfig struct {
	Port    int `doc:"TCP port to listen on."`
	Workers int `doc:"Number of worker goroutines."`
	Cache   struct {
		TTL time.Duration `doc:"How long an entry is kept."`
	} `doc:"Response cache."`
}

// Validate must not change the value; Load calls it on a pointer, so a value receiver works.
func (c limitsConfig) Validate() error {
	if c.Port > 1023 {
		return &config.FieldError{Key: "port", Err: errors.New("must be below 1024")}
	}
	return nil
}

// RemovedKeys is consulted on the top-level type only.
func (limitsConfig) RemovedKeys() map[string]string {
	return map[string]string{"threads": "use workers"}
}

func ExampleValidator() {
	dir, err := os.MkdirTemp("", "config-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("port: 8080\n"), 0o600); err != nil {
		fmt.Println(err)
		return
	}
	_, err = config.Load(limitsConfig{Port: 80, Workers: 4}, config.File(path))
	var fe *config.FieldError
	fmt.Println(errors.As(err, &fe))
	fmt.Println(fe)

	// Output:
	// true
	// port: must be below 1024: "8080" from config.yaml:1
}

func ExampleSchema_Removed() {
	dir, err := os.MkdirTemp("", "config-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("threads: 8\n"), 0o600); err != nil {
		fmt.Println(err)
		return
	}
	_, err = config.Load(limitsConfig{Port: 80, Workers: 4}, config.File(path))
	fmt.Println(err)

	schema, err := config.NewSchema(limitsConfig{}, "")
	if err != nil {
		fmt.Println(err)
		return
	}
	hint, ok := schema.Removed("THREADS")
	fmt.Println(hint, ok)

	// Output:
	// config.yaml:1: key "threads" was removed: use workers
	// use workers true
}

func ExampleSeed() {
	dir, err := os.MkdirTemp("", "config-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "config.yaml")
	defaults := limitsConfig{Port: 80, Workers: 4}
	defaults.Cache.TTL = time.Minute

	created, err := config.Seed(path, defaults)
	if err != nil {
		fmt.Println(err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(created)
	fmt.Print(string(data))

	created, err = config.Seed(path, defaults)
	fmt.Println(created, err)

	// Output:
	// true
	// # TCP port to listen on.
	// port: 80
	// # Number of worker goroutines.
	// workers: 4
	// # Response cache.
	// cache:
	//   # How long an entry is kept.
	//   ttl: 1m0s
	// false <nil>
}

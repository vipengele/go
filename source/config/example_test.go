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

type profile struct {
	Name  string            `doc:"Profile name; layers merge on it."`
	Shell string            `doc:"Shell the profile starts."`
	Env   map[string]string `doc:"Environment of the profile."`
}

type workspaceConfig struct {
	Profiles []profile         `merge:"name" doc:"Named profiles."`
	Labels   map[string]string `merge:"keys" doc:"Labels, merged per key."`
}

func ExampleLoad_collections() {
	dir, err := os.MkdirTemp("", "config-example")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	user := filepath.Join(dir, "user.yaml")
	project := filepath.Join(dir, "project.yaml")
	userFile := "profiles:\n  - name: dev\n    shell: bash\n    env:\n      EDITOR: vim\n  - name: ops\n    shell: sh\nlabels:\n  team: core\n  tier: \"1\"\n"
	projectFile := "profiles:\n  - name: ops\n    shell: zsh\n    env:\n      REGION: eu\n  - name: ci\n    shell: dash\nlabels:\n  tier: \"2\"\n  a.b: dotted\n"
	for path, data := range map[string]string{user: userFile, project: projectFile} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			fmt.Println(err)
			return
		}
	}

	res, err := config.Load(workspaceConfig{}, config.File(user), config.File(project))
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, p := range res.Value().Profiles {
		fmt.Println(p.Name, p.Shell, p.Env)
	}
	fmt.Println(res.Value().Labels)
	for _, s := range res.Sources() {
		fmt.Printf("%s = %s (%s)\n", s.Key, s.Value, s.Origin)
	}

	// Output:
	// dev bash map[EDITOR:vim]
	// ops zsh map[REGION:eu]
	// ci dash map[]
	// map[a.b:dotted team:core tier:2]
	// profiles[0].name = dev (user.yaml:2)
	// profiles[0].shell = bash (user.yaml:3)
	// profiles[0].env["EDITOR"] = vim (user.yaml:5)
	// profiles[1].name = ops (project.yaml:2)
	// profiles[1].shell = zsh (project.yaml:3)
	// profiles[1].env["REGION"] = eu (project.yaml:5)
	// profiles[2].name = ci (project.yaml:6)
	// profiles[2].shell = dash (project.yaml:7)
	// profiles[2].env = {} (project.yaml:6)
	// labels["a.b"] = dotted (project.yaml:10)
	// labels["team"] = core (user.yaml:9)
	// labels["tier"] = 2 (project.yaml:9)
}

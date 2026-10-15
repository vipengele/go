package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"go.vipengele.org/cli/spec"
)

// Kind is one of the directories a tool keeps its files in. The zero value is not a kind.
type Kind int

const (
	// Config holds the configuration a user edits.
	Config Kind = iota + 1
	// State holds what the tool records between runs and a user does not edit.
	State
	// Cache holds what the tool can recreate, so deleting it loses nothing.
	Cache
	// Data holds what the tool keeps for a user and cannot recreate.
	Data
	// Log holds the tool's log files.
	Log
)

// numKinds is the number of kinds, the largest Kind value.
const numKinds = int(Log)

// homeSubdirs are the subdirectory names of the kinds under a home root, indexed by Kind - 1.
func homeSubdirs() [numKinds]string {
	return [numKinds]string{"config", "state", "cache", "data", "log"}
}

// String returns the kind's lowercase name, as in "config", and "Kind(n)" for a value that is
// not a kind.
func (k Kind) String() string {
	if !k.valid() {
		return fmt.Sprintf("Kind(%d)", int(k))
	}
	return homeSubdirs()[k-1]
}

func (k Kind) valid() bool {
	return k >= Config && k <= Log
}

// Options adjusts how [Resolve] and [New] resolve the directories. The zero value resolves the platform
// defaults from the real environment and home directory.
type Options struct {
	// ConfigDir, StateDir, CacheDir, DataDir and LogDir set their kind's directory, used as
	// given. "" leaves the kind to the rest of the precedence.
	ConfigDir, StateDir, CacheDir, DataDir, LogDir string
	// Home is a home root under which every kind without its own option is a subdirectory named
	// as [Kind.String] names it. It takes precedence over the <PREFIX>_HOME variable. "" leaves
	// it to that variable.
	Home string
	// GOOS selects the layout, as a value of runtime.GOOS. "" means runtime.GOOS.
	GOOS string
	// LookupEnv returns an environment variable's value and whether it is set, as os.LookupEnv
	// does. Nil means os.LookupEnv. A variable set to "" counts as unset.
	LookupEnv func(key string) (string, bool)
	// UserHomeDir returns the user's home directory, as os.UserHomeDir does. Nil means
	// os.UserHomeDir. It is called only when some kind needs the home directory.
	UserHomeDir func() (string, error)
}

// Dirs is the resolved directory of each [Kind] and the layout they were resolved for. It is
// created by [Resolve] or [New]. A kind that was not resolved, as every kind in the zero value
// and each kind [Resolve] was not asked for, holds no directory: its accessor returns "" and
// [Dirs.Ensure] reports it.
type Dirs struct {
	goos string
	dirs [numKinds]string
}

// Config returns the directory of the [Config] kind, and "" when it was not resolved.
func (d Dirs) Config() string { return d.dir(Config) }

// State returns the directory of the [State] kind, and "" when it was not resolved.
func (d Dirs) State() string { return d.dir(State) }

// Cache returns the directory of the [Cache] kind, and "" when it was not resolved.
func (d Dirs) Cache() string { return d.dir(Cache) }

// Data returns the directory of the [Data] kind, and "" when it was not resolved.
func (d Dirs) Data() string { return d.dir(Data) }

// Log returns the directory of the [Log] kind, and "" when it was not resolved.
func (d Dirs) Log() string { return d.dir(Log) }

func (d Dirs) dir(k Kind) string {
	return d.dirs[k-1]
}

// ErrNoHomeDir reports that a kind needs the user's home directory and it cannot be found.
var ErrNoHomeDir = errors.New("paths: no home directory")

// New resolves the directory of every [Kind] for the tool id, in the order the package
// documentation gives. It is [Resolve] with no kinds, and fails as Resolve does.
func New(id spec.Identity, opts Options) (Dirs, error) {
	return Resolve(id, opts)
}

// Resolve resolves the directory of each of kinds for the tool id, in the order the package
// documentation gives, and of every [Kind] when kinds is empty. A kind kinds does not name is
// left unresolved in the returned Dirs, and nothing is looked up for it, unless a requested kind
// derives from it, as log does from state on the XDG layout; such a parent is resolved to derive
// its child and is left unresolved itself. Repeating a kind is harmless.
//
// Resolve returns an error wrapping id's [spec.Identity.Validate] error for an invalid id, an
// error naming a value of kinds that is not a Kind, and one wrapping [ErrNoHomeDir] when a
// requested kind, or a kind it derives from, needs the home directory and Options.UserHomeDir
// fails or returns "". It touches nothing on the file system.
func Resolve(id spec.Identity, opts Options, kinds ...Kind) (Dirs, error) {
	if err := id.Validate(); err != nil {
		return Dirs{}, fmt.Errorf("paths: invalid identity: %w", err)
	}
	homeVar, err := id.EnvVar("HOME")
	if err != nil {
		return Dirs{}, fmt.Errorf("paths: invalid identity: %w", err)
	}
	want, err := requested(kinds)
	if err != nil {
		return Dirs{}, err
	}

	r := newResolver(id.DirName, opts)
	explicit := [numKinds]string{opts.ConfigDir, opts.StateDir, opts.CacheDir, opts.DataDir, opts.LogDir}

	root := opts.Home
	if root == "" {
		root = r.env(homeVar)
	}
	var d Dirs
	d.goos = r.goos
	if root != "" {
		for i, sub := range homeSubdirs() {
			if !want[i] {
				continue
			}
			d.dirs[i] = explicit[i]
			if d.dirs[i] == "" {
				d.dirs[i] = filepath.Join(root, sub)
			}
		}
		return d, nil
	}

	d.dirs, err = r.layout(explicit, want)
	if err != nil {
		return Dirs{}, err
	}
	return d, nil
}

// requested returns which kinds kinds names, indexed by Kind - 1, and every kind when kinds is
// empty. It fails on a value that is not a [Kind].
func requested(kinds []Kind) ([numKinds]bool, error) {
	var want [numKinds]bool
	if len(kinds) == 0 {
		for i := range want {
			want[i] = true
		}
		return want, nil
	}
	for _, k := range kinds {
		if !k.valid() {
			return [numKinds]bool{}, fmt.Errorf("paths: invalid kind %v", k)
		}
		want[k-1] = true
	}
	return want, nil
}

// resolver carries what the layouts read: the environment, the home directory, looked up once
// and only on first use, and the application directory name.
type resolver struct {
	goos        string
	app         string
	lookupEnv   func(string) (string, bool)
	userHomeDir func() (string, error)

	homeLooked bool
	home       string
	homeErr    error
}

func newResolver(app string, opts Options) *resolver {
	r := &resolver{
		goos:        opts.GOOS,
		app:         app,
		lookupEnv:   opts.LookupEnv,
		userHomeDir: opts.UserHomeDir,
	}
	if r.goos == "" {
		r.goos = runtime.GOOS
	}
	if r.lookupEnv == nil {
		r.lookupEnv = os.LookupEnv
	}
	if r.userHomeDir == nil {
		r.userHomeDir = os.UserHomeDir
	}
	return r
}

// env returns the value of the variable key, and "" when it is unset or set to "".
func (r *resolver) env(key string) string {
	v, _ := r.lookupEnv(key)
	return v
}

// homeDir returns the user's home directory, calling UserHomeDir at most once.
func (r *resolver) homeDir() (string, error) {
	if !r.homeLooked {
		r.homeLooked = true
		r.home, r.homeErr = r.userHomeDir()
		switch {
		case r.homeErr != nil:
			r.homeErr = fmt.Errorf("%w: %w", ErrNoHomeDir, r.homeErr)
		case r.home == "":
			r.homeErr = ErrNoHomeDir
		}
	}
	return r.home, r.homeErr
}

// underHome joins elem to the user's home directory.
func (r *resolver) underHome(elem ...string) (string, error) {
	home, err := r.homeDir()
	if err != nil {
		return "", err // [lydite:exclude_from_mutation][every caller discards the path when the error is set, so the value returned with ErrNoHomeDir is unobservable]
	}
	return filepath.Join(append([]string{home}, elem...)...), nil
}

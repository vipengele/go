package paths

import (
	"path/filepath"
)

// layout resolves the kinds want marks, indexed by Kind - 1, for r's GOOS, below no home root.
// explicit holds the explicit option of each kind, indexed the same way, and a kind's default is
// resolved only when its option is "". A kind want does not mark is left "".
func (r *resolver) layout(explicit [numKinds]string, want [numKinds]bool) ([numKinds]string, error) {
	var def defaultDir
	switch r.goos {
	case "darwin":
		def = r.darwin
	case "windows":
		def = r.windows
	default:
		def = r.xdg
	}
	return resolveKinds(explicit, want, def)
}

// defaultDir returns the default directory of kind k on a layout. A kind derived from another
// reads that kind through dir, which resolves it with its explicit option taking precedence.
type defaultDir func(k Kind, dir func(Kind) (string, error)) (string, error)

// pick returns the explicit option when it is set, and calls def otherwise.
func pick(explicit string, def func() (string, error)) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	return def()
}

// resolveKinds resolves the kinds want marks, in Kind order, and stops at the first error. A kind
// is resolved only when it is marked or a marked kind derives from it, so the failure of a kind
// nothing asks for is never seen.
func resolveKinds(explicit [numKinds]string, want [numKinds]bool, def defaultDir) ([numKinds]string, error) {
	var dir func(Kind) (string, error)
	dir = func(k Kind) (string, error) {
		return pick(explicit[k-1], func() (string, error) { return def(k, dir) })
	}
	var dirs [numKinds]string
	for i, wanted := range want {
		if !wanted {
			continue
		}
		d, err := dir(Kind(i + 1))
		if err != nil {
			return [numKinds]string{}, err
		}
		dirs[i] = d
	}
	return dirs, nil
}

// xdg resolves the XDG layout: each kind but log under its XDG base directory or its fallback
// below the home directory, and log under the resolved state directory.
func (r *resolver) xdg(k Kind, dir func(Kind) (string, error)) (string, error) {
	base := func(envVar string, fallback ...string) (string, error) {
		if v := r.env(envVar); filepath.IsAbs(v) {
			return filepath.Join(v, r.app), nil
		}
		return r.underHome(append(fallback, r.app)...)
	}
	switch k {
	case Config:
		return base("XDG_CONFIG_HOME", ".config")
	case State:
		return base("XDG_STATE_HOME", ".local", "state")
	case Cache:
		return base("XDG_CACHE_HOME", ".cache")
	case Data:
		return base("XDG_DATA_HOME", ".local", "share")
	default:
		state, err := dir(State)
		if err != nil {
			return "", err // [lydite:exclude_from_mutation][resolveKinds discards the path when the error is set, so the value returned with it is unobservable]
		}
		return filepath.Join(state, "log"), nil
	}
}

// darwin resolves the macOS layout: config under Application Support, state and data under the
// resolved config directory, cache under Caches and log under Logs.
func (r *resolver) darwin(k Kind, dir func(Kind) (string, error)) (string, error) {
	switch k {
	case Config:
		return r.underHome("Library", "Application Support", r.app)
	case State, Data:
		config, err := dir(Config)
		if err != nil {
			return "", err // [lydite:exclude_from_mutation][resolveKinds discards the path when the error is set, so the value returned with it is unobservable]
		}
		return filepath.Join(config, k.String()), nil
	case Cache:
		return r.underHome("Library", "Caches", r.app)
	default:
		return r.underHome("Library", "Logs", r.app)
	}
}

// windows resolves the Windows layout: config under the roaming application data directory, and
// every other kind a subdirectory of the application's directory under the local one.
func (r *resolver) windows(k Kind, _ func(Kind) (string, error)) (string, error) {
	appDir := func(envVar string, fallback ...string) (string, error) {
		if v := r.env(envVar); v != "" {
			return filepath.Join(v, r.app), nil
		}
		return r.underHome(append(fallback, r.app)...)
	}
	if k == Config {
		return appDir("AppData", "AppData", "Roaming")
	}
	local, err := appDir("LocalAppData", "AppData", "Local")
	if err != nil {
		return "", err // [lydite:exclude_from_mutation][resolveKinds discards the path when the error is set, so the value returned with it is unobservable]
	}
	return filepath.Join(local, k.String()), nil
}

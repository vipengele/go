package paths

import (
	"path/filepath"
)

// layout resolves every kind for r's GOOS, below a home root. explicit holds the explicit option
// of each kind, indexed by Kind - 1, and a kind's default is resolved only when its option is "".
func (r *resolver) layout(explicit [numKinds]string) ([numKinds]string, error) {
	switch r.goos {
	case "darwin":
		return r.darwin(explicit)
	case "windows":
		return r.windows(explicit)
	default:
		return r.xdg(explicit)
	}
}

// pick returns the explicit option when it is set, and calls def otherwise.
func pick(explicit string, def func() (string, error)) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	return def()
}

// resolveAll resolves the kinds in order, so a derived kind reads the kind it derives from as
// already resolved. It stops at the first error.
func resolveAll(explicit [numKinds]string, order []Kind, def func(Kind, *[numKinds]string) (string, error)) ([numKinds]string, error) {
	var dirs [numKinds]string
	for _, k := range order {
		dir, err := pick(explicit[k-1], func() (string, error) { return def(k, &dirs) })
		if err != nil {
			return [numKinds]string{}, err
		}
		dirs[k-1] = dir
	}
	return dirs, nil
}

// xdg resolves the XDG layout: each kind but log under its XDG base directory or its fallback
// below the home directory, and log under the resolved state directory.
func (r *resolver) xdg(explicit [numKinds]string) ([numKinds]string, error) {
	base := func(envVar string, fallback ...string) (string, error) {
		if v := r.env(envVar); filepath.IsAbs(v) {
			return filepath.Join(v, r.app), nil
		}
		return r.underHome(append(fallback, r.app)...)
	}
	order := []Kind{Config, State, Cache, Data, Log}
	return resolveAll(explicit, order, func(k Kind, dirs *[numKinds]string) (string, error) {
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
			return filepath.Join(dirs[State-1], "log"), nil
		}
	})
}

// darwin resolves the macOS layout: config under Application Support, state and data under the
// resolved config directory, cache under Caches and log under Logs.
func (r *resolver) darwin(explicit [numKinds]string) ([numKinds]string, error) {
	order := []Kind{Config, State, Data, Cache, Log}
	return resolveAll(explicit, order, func(k Kind, dirs *[numKinds]string) (string, error) {
		switch k {
		case Config:
			return r.underHome("Library", "Application Support", r.app)
		case State:
			return filepath.Join(dirs[Config-1], "state"), nil
		case Data:
			return filepath.Join(dirs[Config-1], "data"), nil
		case Cache:
			return r.underHome("Library", "Caches", r.app)
		default:
			return r.underHome("Library", "Logs", r.app)
		}
	})
}

// windows resolves the Windows layout: config under the roaming application data directory, and
// every other kind a subdirectory of the application's directory under the local one.
func (r *resolver) windows(explicit [numKinds]string) ([numKinds]string, error) {
	appDir := func(envVar string, fallback ...string) (string, error) {
		if v := r.env(envVar); v != "" {
			return filepath.Join(v, r.app), nil
		}
		return r.underHome(append(fallback, r.app)...)
	}
	order := []Kind{Config, State, Cache, Data, Log}
	return resolveAll(explicit, order, func(k Kind, _ *[numKinds]string) (string, error) {
		if k == Config {
			return appDir("AppData", "AppData", "Roaming")
		}
		local, err := appDir("LocalAppData", "AppData", "Local")
		if err != nil {
			return "", err
		}
		return filepath.Join(local, k.String()), nil
	})
}

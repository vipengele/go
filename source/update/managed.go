package update

import (
	"fmt"
	"strings"
)

// Install is the externally managed install a [Detector] reports.
type Install struct {
	// Manager names what manages the install, such as "Homebrew" or "Nix".
	Manager string
	// Instead is a full clause saying what to do instead of updating the binary in place, such
	// as "run `brew upgrade atk`".
	Instead string
}

// Detector reports whether the resolved path of the running executable belongs to an externally
// managed install, and which one. It returns false when the path is not one it recognises.
type Detector func(path string) (Install, bool)

// CheckManaged returns a [*ManagedInstallError] when the running executable is an externally
// managed install, and nil when it is not. The path [Config.Executable] returns is resolved
// through [Config.EvalSymlinks] and matched, in order, against Nix, Homebrew, the container check
// and then [Config.Detectors]; the first match wins, and the built-in ones cannot be turned off.
// A path whose symlinks cannot be resolved is matched as it is, so a transient filesystem error
// never blocks an update.
//
// Paths are matched on a directory boundary and case-sensitively, with "/" separators: the
// install locations the built-in detectors know are Unix paths, so a Windows executable never
// matches them. Only the default Nix and Homebrew prefixes are known.
func (c Config) CheckManaged() error {
	r, err := c.resolve()
	if err != nil {
		return err
	}

	path, err := r.Executable()
	if err != nil {
		return fmt.Errorf("update: locate executable: %w", err)
	}

	resolved, err := r.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}

	detectors := []Detector{
		detectPrefixes("Nix", nixInstead(r.BinaryName), []string{"/nix/store"}),
		detectPrefixes("Homebrew", "run `brew upgrade "+r.BinaryName+"`", []string{"/opt/homebrew", "/usr/local/Cellar", "/usr/local/Caskroom", "/home/linuxbrew/.linuxbrew"}),
		func(string) (Install, bool) {
			return Install{Manager: "a container", Instead: "pull a newer image of the container"}, r.InContainer()
		},
	}
	detectors = append(detectors, r.Detectors...)

	for _, detect := range detectors {
		if install, ok := detect(resolved); ok {
			return &ManagedInstallError{Path: path, Resolved: resolved, Manager: install.Manager, Instead: install.Instead}
		}
	}

	return nil
}

// nixInstead is the clause for a Nix install of binary. It stays generic because the right command
// depends on how Nix is used: `nix profile upgrade` is wrong on NixOS and under home-manager.
func nixInstead(binary string) string {
	return "update it through your Nix configuration (`nixos-rebuild switch`, `home-manager switch` or `nix profile upgrade " + binary + "`)"
}

// detectPrefixes returns a [Detector] reporting manager, with instead, for a path under one of
// prefixes.
func detectPrefixes(manager, instead string, prefixes []string) Detector {
	return func(path string) (Install, bool) {
		for _, prefix := range prefixes {
			if hasPathPrefix(path, prefix) {
				return Install{Manager: manager, Instead: instead}, true
			}
		}

		return Install{}, false
	}
}

// hasPathPrefix reports whether path is prefix or lies under it, so that "/opt/homebrewX" is not
// under "/opt/homebrew".
func hasPathPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

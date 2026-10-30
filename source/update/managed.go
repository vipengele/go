package update

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

package update

// Notice names a release newer than the running one.
type Notice struct {
	// Name is the tool's name, the Identity's Name.
	Name string
	// Current is the running program's version.
	Current string
	// Latest is the newer release's version.
	Latest string
	// Instead is the clause of the [*ManagedInstallError] the running executable gets, saying how
	// to update an externally managed install, or empty when the install is not managed or the
	// check could not tell. When set, String gives it in place of the update command, which would
	// refuse a managed install.
	Instead string
}

// String is the one-line announcement of the release, without a trailing newline.
func (n Notice) String() string {
	available := n.Name + " " + n.Latest + " is available (you're on " + n.Current + ")"
	if n.Instead != "" {
		return available + "; " + n.Instead + "."
	}
	return available + ". Run '" + n.Name + " update' to install."
}

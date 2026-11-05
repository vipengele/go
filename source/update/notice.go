package update

// Notice names a release newer than the running one.
type Notice struct {
	// Name is the tool's name, the Identity's Name.
	Name string
	// Current is the running program's version.
	Current string
	// Latest is the newer release's version.
	Latest string
}

// String is the one-line announcement of the release, without a trailing newline.
func (n Notice) String() string {
	return n.Name + " " + n.Latest + " is available (you're on " + n.Current + "). Run '" + n.Name +
		" update' to install."
}

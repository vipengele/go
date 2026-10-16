package spec

// UsageError is an error a run function returns when the command line it was given cannot
// proceed: a question that needs an answer the user cannot type, because no terminal is there to
// ask it on. A caller finds it anywhere in an error chain with errors.As; go.vipengele.org/cli/core
// exits 2 for it, the code of a command line the tree does not accept. It wraps nothing.
type UsageError struct {
	// Question is the question the run needs answered, printed first.
	Question string
	// Flag is the flag that answers Question without asking, printed as given, such as "--yes".
	// Empty names no flag.
	Flag string
}

// Error is "<Question> requires interactive input; pass <Flag>", or
// "<Question> requires interactive input" when Flag is empty.
func (e *UsageError) Error() string {
	msg := e.Question + " requires interactive input"
	if e.Flag == "" {
		return msg
	}
	return msg + "; pass " + e.Flag
}

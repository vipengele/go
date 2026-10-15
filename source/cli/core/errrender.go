package cli

import "go.vipengele.org/cli/core/internal/errrender"

// typedRenderer returns the renderer of an error that is an E, by type assertion on the error
// itself rather than errors.As over its chain, so it renders the node of the tree that holds the
// E and nothing that merely wraps one.
func typedRenderer[E error](fn func(E) string) errrender.Renderer {
	return func(err error) (string, bool) {
		e, ok := err.(E)
		if !ok {
			return "", false
		}
		return fn(e), true
	}
}

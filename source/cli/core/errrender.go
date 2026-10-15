package cli

import "go.vipengele.org/cli/core/internal/errrender"

// typedRenderer returns the renderer of an error that is an E, by type assertion on the error
// itself rather than errors.As over its chain, so it renders the node of the tree that holds the
// E and nothing that merely wraps one.
func typedRenderer[E error](fn func(E) string) errrender.Renderer {
	return func(err error) (string, bool) {
		e, ok := err.(E)
		if !ok {
			return "", false // [lydite:exclude_from_mutation][errrender.Render skips a renderer's output unless the bool is true and the text is non-empty, so neither value returned for an error that is not an E is observable]
		}
		return fn(e), true
	}
}

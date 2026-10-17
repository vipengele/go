package config

import (
	"context"

	cfg "go.vipengele.org/config"
)

// ctxKey is the context key of a loaded configuration of type T. Each T has a key of its own, so
// a Result of one type is never read as another.
type ctxKey[T any] struct{}

// With returns a copy of ctx carrying r, which [From] with the same T returns.
func With[T any](ctx context.Context, r cfg.Result[T]) context.Context {
	return context.WithValue(ctx, ctxKey[T]{}, r)
}

// From returns the configuration of type T that [With] stored in ctx, and whether ctx carries one.
// It reports false for a context no configuration was stored in, and for one that carries a
// configuration of another type only.
func From[T any](ctx context.Context) (cfg.Result[T], bool) {
	r, ok := ctx.Value(ctxKey[T]{}).(cfg.Result[T])
	return r, ok
}

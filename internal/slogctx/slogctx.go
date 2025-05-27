// Package slogctx provides a minimal mechanism for passing a [slog.Logger]
// around in contexts.
package slogctx

import (
	"context"
	"log/slog"
)

type ctxKey int

const slogCtxKey ctxKey = iota

// New creates a new child [context.Context] containing the given [slog.Logger]
func New(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, slogCtxKey, logger)
}

// From returns the [slog.Logger] from the given [context.Context], or the
// default logger if not found.
func From(ctx context.Context) *slog.Logger {
	val := ctx.Value(slogCtxKey)
	if val == nil {
		return slog.Default()
	}
	return val.(*slog.Logger)
}

// Debug logs at debug level.
func Debug(ctx context.Context, msg string, args ...any) {
	log(ctx, slog.LevelDebug, msg, args...)
}

func log(ctx context.Context, level slog.Level, msg string, args ...any) {
	From(ctx).Log(ctx, level, msg, args...)
}

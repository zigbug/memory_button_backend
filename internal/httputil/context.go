package httputil

import (
	"context"

	"memorybutton/backend/internal/auth"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	claimsKey
)

func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func ClaimsFrom(ctx context.Context) *auth.Claims {
	if v, ok := ctx.Value(claimsKey).(*auth.Claims); ok {
		return v
	}
	return nil
}

func WithClaims(ctx context.Context, c *auth.Claims) context.Context {
	return context.WithValue(ctx, claimsKey, c)
}

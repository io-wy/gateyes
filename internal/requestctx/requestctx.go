package requestctx

import "context"

type contextKey string

const (
	keyRequestID contextKey = "request_id"
	keySessionID contextKey = "session_id"
	keyTokenID   contextKey = "token_id"
)

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, keyRequestID, requestID)
}

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(keyRequestID).(string)
	return v
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, keySessionID, sessionID)
}

func SessionID(ctx context.Context) string {
	v, _ := ctx.Value(keySessionID).(string)
	return v
}

func WithTokenID(ctx context.Context, tokenID string) context.Context {
	return context.WithValue(ctx, keyTokenID, tokenID)
}

func TokenID(ctx context.Context) string {
	v, _ := ctx.Value(keyTokenID).(string)
	return v
}

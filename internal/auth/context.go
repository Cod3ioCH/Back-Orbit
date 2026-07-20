package auth

import "context"

type contextKey int

const (
	sessionContextKey contextKey = iota
	userContextKey
)

func withSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// SessionFromContext returns the session attached by RequireSession, if any.
func SessionFromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionContextKey).(Session)
	return session, ok
}

func withUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext returns the authenticated user attached by RequireSession,
// if any.
func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok
}

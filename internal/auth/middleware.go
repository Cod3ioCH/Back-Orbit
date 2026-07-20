package auth

import (
	"encoding/json"
	"net/http"
)

// Authenticator wires the session/user stores into HTTP middleware.
type Authenticator struct {
	Sessions   *SessionStore
	Users      *UserStore
	CookieName string
}

// RequireSession validates the session cookie and attaches the session and
// user to the request context. It responds 401 if the session is missing,
// invalid, or expired.
func (a *Authenticator) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := TokenFromCookie(r, a.CookieName)

		session, err := a.Sessions.Validate(r.Context(), token)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}

		user, err := a.Users.GetByID(r.Context(), session.UserID)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
			return
		}

		ctx := withSession(r.Context(), session)
		ctx = withUser(ctx, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// SecurityHeaders sets defensive HTTP response headers on every response:
// a restrictive Content-Security-Policy (the frontend is same-origin and
// needs no external script/style/font/image sources), protections against
// MIME-sniffing and clickjacking, and a conservative Referrer-Policy. HSTS
// is only set when the connection is known to be HTTPS (directly or via a
// trusted reverse proxy), since sending it over plain HTTP would be
// misleading and could lock out a misconfigured deployment.
func SecurityHeaders(hstsEnabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			if hstsEnabled {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

package auth

import (
	"net/http"
	"time"
)

// SetSessionCookie writes the session cookie. Secure is expected to be true
// whenever the connection is HTTPS, either terminated locally or reported by
// a trusted reverse proxy (see config.Config.TrustProxyHeaders).
func SetSessionCookie(w http.ResponseWriter, name, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie, e.g. on logout.
func ClearSessionCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// TokenFromCookie extracts the session token from the request, if present.
func TokenFromCookie(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

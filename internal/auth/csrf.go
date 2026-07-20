package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// CSRF protection uses the double-submit cookie pattern: a random token is
// set as a readable (non-HttpOnly) cookie, and the frontend must echo it
// back in a request header on every state-changing request. Because the
// token itself is never accepted from the request body or query string, and
// because the comparison happens server-side against the cookie value the
// browser sent, a cross-site request cannot forge a match without first
// being able to read the cookie (which the same-origin policy prevents).
const (
	CSRFCookieName = "backorbit_csrf"
	CSRFHeaderName = "X-CSRF-Token"
)

var csrfSafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

func generateCSRFToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EnsureCSRFCookie sets a CSRF cookie on the response if the request doesn't
// already carry a valid one. It should run early in the middleware chain,
// before CSRFProtect, on every request (including GETs) so the frontend
// always has a token to echo back. isSecure is evaluated per-request (rather
// than fixed at startup) so it can reflect either a direct TLS connection or
// a trusted reverse proxy's forwarded-proto header.
func EnsureCSRFCookie(isSecure func(r *http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := r.Cookie(CSRFCookieName); err != nil {
				token, genErr := generateCSRFToken()
				if genErr != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     CSRFCookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: false, // the frontend must be able to read this to echo it back
					Secure:   isSecure(r),
					SameSite: http.SameSiteLaxMode,
				})
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFProtect rejects state-changing requests (anything but GET/HEAD/OPTIONS)
// whose X-CSRF-Token header does not match the backorbit_csrf cookie.
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfSafeMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "missing CSRF cookie", http.StatusForbidden)
			return
		}

		header := r.Header.Get(CSRFHeaderName)
		if header == "" || subtle.ConstantTimeCompare([]byte(header), []byte(cookie.Value)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

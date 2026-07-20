package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func alwaysInsecure(*http.Request) bool { return false }

func TestEnsureCSRFCookieSetsCookieOnFirstRequest(t *testing.T) {
	handler := EnsureCSRFCookie(alwaysInsecure)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == CSRFCookieName {
			found = true
			if c.Value == "" {
				t.Fatal("expected a non-empty CSRF token")
			}
			if c.HttpOnly {
				t.Fatal("CSRF cookie must not be HttpOnly, the frontend needs to read it")
			}
		}
	}
	if !found {
		t.Fatal("expected a CSRF cookie to be set")
	}
}

func TestCSRFProtectAllowsSafeMethodsWithoutToken(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected GET to pass without a CSRF token, got status %d", rec.Code)
	}
}

func TestCSRFProtectBlocksStateChangingRequestWithoutToken(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected POST without a CSRF token to be forbidden, got status %d", rec.Code)
	}
}

func TestCSRFProtectBlocksMismatchedToken(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "cookie-value"})
	req.Header.Set(CSRFHeaderName, "different-value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched CSRF token to be forbidden, got status %d", rec.Code)
	}
}

func TestCSRFProtectAllowsMatchingToken(t *testing.T) {
	handler := CSRFProtect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: "matching-value"})
	req.Header.Set(CSRFHeaderName, "matching-value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected matching CSRF token to be allowed, got status %d", rec.Code)
	}
}

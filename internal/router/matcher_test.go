package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatchers(t *testing.T) {
	t.Run("MatchPathPrefix", func(t *testing.T) {
		matcher := MatchPathPrefix("/api/v1")
		req, _ := http.NewRequest("GET", "/api/v1/users", nil)
		if !matcher(req) {
			t.Error("Expected PathPrefix to match")
		}
		req2, _ := http.NewRequest("GET", "/api/v2/users", nil)
		if matcher(req2) {
			t.Error("Expected PathPrefix to not match")
		}
	})

	t.Run("MatchPath", func(t *testing.T) {
		matcher := MatchPath("/api/v1/users")
		req, _ := http.NewRequest("GET", "/api/v1/users", nil)
		if !matcher(req) {
			t.Error("Expected Path to match")
		}
		req2, _ := http.NewRequest("GET", "/api/v1/users/", nil)
		if matcher(req2) {
			t.Error("Expected Path to not match due to trailing slash")
		}
	})

	t.Run("MatchMethod", func(t *testing.T) {
		matcher := MatchMethod("POST")
		req, _ := http.NewRequest("POST", "/test", nil)
		if !matcher(req) {
			t.Error("Expected Method to match")
		}
		req2, _ := http.NewRequest("GET", "/test", nil)
		if matcher(req2) {
			t.Error("Expected Method to not match")
		}
	})

	t.Run("MatchHeader", func(t *testing.T) {
		matcher := MatchHeader("X-Internal", "true")
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Internal", "true")
		if !matcher(req) {
			t.Error("Expected Header to match")
		}
		
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.Header.Set("X-Internal", "false")
		if matcher(req2) {
			t.Error("Expected Header to not match")
		}
	})

	t.Run("MatchClientIP", func(t *testing.T) {
		matcher := MatchClientIP("192.168.1.0/24")
		
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Real-IP", "192.168.1.100")
		if !matcher(req) {
			t.Error("Expected CIDR IP to match")
		}

		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.Header.Set("X-Real-IP", "10.0.0.5")
		if matcher(req2) {
			t.Error("Expected CIDR IP to not match")
		}

		exactMatcher := MatchClientIP("10.0.0.5")
		if !exactMatcher(req2) {
			t.Error("Expected exact IP to match")
		}
	})

	t.Run("Compound Logic", func(t *testing.T) {
		matcher := MatchAnd(MatchPathPrefix("/api"), MatchMethod("POST"))
		
		req1, _ := http.NewRequest("POST", "/api/data", nil)
		if !matcher(req1) {
			t.Error("Expected AND to match")
		}

		req2, _ := http.NewRequest("GET", "/api/data", nil)
		if matcher(req2) {
			t.Error("Expected AND to not match due to method GET")
		}

		orMatcher := MatchOr(MatchPath("/login"), MatchPath("/signup"))
		
		req3, _ := http.NewRequest("GET", "/login", nil)
		req4, _ := http.NewRequest("GET", "/forgot", nil)
		
		if !orMatcher(req3) {
			t.Error("Expected OR to match login")
		}
		if orMatcher(req4) {
			t.Error("Expected OR to not match forgot")
		}
	})
}

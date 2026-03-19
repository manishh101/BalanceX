package router

import (
	"net"
	"net/http"
	"strings"
)

// MatcherFunc evaluates an HTTP request against a routing rule.
// It returns true if the request matches the rule.
type MatcherFunc func(req *http.Request) bool

// MatchPathPrefix creates a MatcherFunc that checks if the request path starts with the prefix.
func MatchPathPrefix(prefix string) MatcherFunc {
	return func(req *http.Request) bool {
		return strings.HasPrefix(req.URL.Path, prefix)
	}
}

// MatchPath creates a MatcherFunc that checks for an exact match of the request path.
func MatchPath(path string) MatcherFunc {
	return func(req *http.Request) bool {
		return req.URL.Path == path
	}
}

// MatchMethod creates a MatcherFunc that checks the HTTP method.
func MatchMethod(method string) MatcherFunc {
	method = strings.ToUpper(method)
	return func(req *http.Request) bool {
		return req.Method == method
	}
}

// MatchHeader creates a MatcherFunc that checks if a specific header has the expected value.
func MatchHeader(key, value string) MatcherFunc {
	return func(req *http.Request) bool {
		return req.Header.Get(key) == value
	}
}

// MatchClientIP creates a MatcherFunc that checks if the client IP belongs to the given CIDR or exactly matches the IP.
func MatchClientIP(ipRange string) MatcherFunc {
	_, ipNet, err := net.ParseCIDR(ipRange)
	
	if err == nil {
		// Valid CIDR notation
		return func(req *http.Request) bool {
			ip := getClientIP(req)
			parsedIP := net.ParseIP(ip)
			if parsedIP == nil {
				return false
			}
			return ipNet.Contains(parsedIP)
		}
	}

	// Maybe it's a direct IP
	expectedIP := net.ParseIP(ipRange)
	if expectedIP != nil {
		return func(req *http.Request) bool {
			ip := getClientIP(req)
			return expectedIP.Equal(net.ParseIP(ip))
		}
	}

	// Invalid IP or CIDR — return a matcher that always fails
	return func(req *http.Request) bool {
		return false
	}
}

// MatchAnd creates a MatcherFunc that returns true only if both child matchers return true.
func MatchAnd(left, right MatcherFunc) MatcherFunc {
	return func(req *http.Request) bool {
		return left(req) && right(req)
	}
}

// MatchOr creates a MatcherFunc that returns true if either child matcher returns true.
func MatchOr(left, right MatcherFunc) MatcherFunc {
	return func(req *http.Request) bool {
		return left(req) || right(req)
	}
}

// getClientIP extracts the real client IP from the request, respecting X-Real-IP if set.
func getClientIP(req *http.Request) string {
	if realIP := req.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	// Fallback to RemoteAddr (which includes port, so we need to split it)
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		return host
	}
	return req.RemoteAddr
}

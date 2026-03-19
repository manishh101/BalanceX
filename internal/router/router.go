package router

import (
	"fmt"
	"net/http"
	"sort"
)

// Route represents a configured routing rule and its target destination.
type Route struct {
	Name        string
	RuleString  string
	Priority    int
	Middlewares []string
	Service     string
	Match       MatcherFunc
	Handler     http.Handler // The compiled handler (middlewares + proxy)
}

// Manager holds all configured routes and evaluates incoming requests against them.
type Manager struct {
	routes []*Route
}

// NewManager creates a new empty router manager.
func NewManager() *Manager {
	return &Manager{}
}

// AddRoute parses a rule expression and registers a new route.
// Routes are automatically sorted by priority (descending). If priorities are equal,
// the route with the longest rule string is evaluated first (Traefik-inspired determinism).
func (m *Manager) AddRoute(name, ruleStr string, priority int, middlewares []string, service string, handler http.Handler) error {
	matcher, err := ParseRule(ruleStr)
	if err != nil {
		return fmt.Errorf("failed to parse rule for router %q: %w", name, err)
	}

	m.routes = append(m.routes, &Route{
		Name:        name,
		RuleString:  ruleStr,
		Priority:    priority,
		Middlewares: middlewares,
		Service:     service,
		Match:       matcher,
		Handler:     handler,
	})

	// Sort routes: highest priority first. If tied, longest rule string wins.
	sort.Slice(m.routes, func(i, j int) bool {
		if m.routes[i].Priority == m.routes[j].Priority {
			return len(m.routes[i].RuleString) > len(m.routes[j].RuleString)
		}
		return m.routes[i].Priority > m.routes[j].Priority
	})

	return nil
}

// Route evaluates an HTTP request against all registered routes in order.
// Returns the matching Route, or nil if no routes match the request.
func (m *Manager) Route(req *http.Request) *Route {
	for _, r := range m.routes {
		if r.Match(req) {
			return r
		}
	}
	return nil
}

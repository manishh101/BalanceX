package middleware

import "net/http"

// Middleware is a function that wraps an http.Handler to add processing
// before and/or after the next handler in the chain.
// This pattern mirrors Traefik's middleware architecture.
type Middleware func(http.Handler) http.Handler

// Chain composes multiple middlewares into a single Middleware.
// Middlewares execute left-to-right: Chain(A, B, C)(handler)
// produces A(B(C(handler))), so the request flows A → B → C → handler.
func Chain(middlewares ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

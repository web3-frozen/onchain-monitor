package middleware

import (
	"net/http"
	"strings"
)

// CORS allows the configured origin plus any Vercel preview deployment.
func CORS(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqOrigin := r.Header.Get("Origin")
			allowed := origin

			if reqOrigin != "" && isAllowed(reqOrigin, origin) {
				allowed = reqOrigin
			}

			w.Header().Set("Access-Control-Allow-Origin", allowed)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isAllowed(reqOrigin, configured string) bool {
	if configured == "*" {
		return true
	}
	if reqOrigin == configured {
		return true
	}
	// Allow Vercel PR preview deployments for this project only
	if strings.HasPrefix(reqOrigin, "https://onchain-monitor-frontend-") &&
		strings.HasSuffix(reqOrigin, "-dummysuis-projects.vercel.app") {
		return true
	}
	return false
}

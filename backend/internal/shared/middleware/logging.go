// Package middleware contains shared HTTP middleware used across all domain routes.
package middleware

import (
	"log"
	"net/http"
	"time"
)

// Logging returns a middleware that logs each request's method, path, and duration.
func Logging(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).String())
		})
	}
}

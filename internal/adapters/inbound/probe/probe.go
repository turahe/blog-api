// Package probe serves plain net/http liveness and readiness endpoints for processes without
// the public API router, such as the worker.
package probe

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/turahe/blog-api/internal/core/health/domain"
	"github.com/turahe/blog-api/internal/core/health/ports"
)

// Live reports that the process is up; it never checks dependencies.
func Live(svc ports.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, svc.Live())
	})
}

// Ready probes dependencies and answers 503 when a critical one is down. Check errors are
// logged, never written to the response.
func Ready(svc ports.Service, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := svc.Ready(r.Context())

		code := http.StatusOK
		if status.Status != "ok" {
			code = http.StatusServiceUnavailable
		}

		for _, dependency := range status.Dependencies {
			if dependency.Err != nil {
				logger.WarnContext(r.Context(), "readiness check failed",
					"dependency", dependency.Name, "error", dependency.Err)
			}
		}

		write(w, code, status)
	})
}

func write(w http.ResponseWriter, code int, status domain.Status) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(status)
}

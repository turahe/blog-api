package handlers

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
)

// Live returns the liveness probe handler.
func Live(health healthports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		responses.Success(c, nethttp.StatusOK, health.Live())
	}
}

// Ready returns the readiness probe handler.
func Ready(health healthports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := health.Ready(c.Request.Context())
		httpStatus := nethttp.StatusOK
		if status.Status != "ok" {
			httpStatus = nethttp.StatusServiceUnavailable
		}
		responses.Success(c, httpStatus, status)
	}
}

// Version returns the build version handler.
func Version(version string) gin.HandlerFunc {
	return func(c *gin.Context) {
		responses.Success(c, nethttp.StatusOK, gin.H{"version": version})
	}
}

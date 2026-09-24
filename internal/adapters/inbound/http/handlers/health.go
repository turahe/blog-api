package handlers

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	healthports "github.com/turahe/blog-api/internal/core/health/ports"
)

// Live returns the liveness probe handler (also used as /api/v1/health alias).
// Live godoc
//
//	@Summary	Liveness probe
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Router		/health/live [get]
func Live(health healthports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		responses.Success(c, nethttp.StatusOK, health.Live())
	}
}

// ready godoc
//
//	@Summary	Readiness probe
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	503	{object}	responses.Envelope
//	@Router		/health/ready [get]
func ready(health healthports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := health.Ready(c.Request.Context())
		httpStatus := nethttp.StatusOK
		if status.Status != "ok" {
			httpStatus = nethttp.StatusServiceUnavailable
		}
		responses.Success(c, httpStatus, status)
	}
}

// version godoc
//
//	@Summary	Build version
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Router		/health/version [get]
func version(buildVersion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		responses.Success(c, nethttp.StatusOK, gin.H{"version": buildVersion})
	}
}

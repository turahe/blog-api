package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	"github.com/turahe/blog-api/internal/adapters/inbound/routes"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

// ingestMaxBodyBytes caps an ingest request body; one event is well under 1 KiB.
const ingestMaxBodyBytes = 8 << 10

// Context keys set by analyticsIngestGate.
const (
	contextAnalyticsSubject = "analytics.subject"
	contextAnalyticsRefused = "analytics.refused"
)

type analyticsIngestAPI interface {
	PageView(ctx context.Context, meta analyticsservice.Meta, in analyticsservice.PageViewInput) (uuid.UUID, error)
	TimeSpent(ctx context.Context, meta analyticsservice.Meta, in analyticsservice.TimeSpentInput) (uuid.UUID, error)
	Navigation(ctx context.Context, meta analyticsservice.Meta, in analyticsservice.NavigationInput) (uuid.UUID, error)
	Search(ctx context.Context, meta analyticsservice.Meta, in analyticsservice.SearchInput) (uuid.UUID, error)
	SearchClick(ctx context.Context, meta analyticsservice.Meta, in analyticsservice.SearchClickInput) (uuid.UUID, error)
}

// ingestPageViewHandler godoc
//
//	@Summary		Record a page view
//	@Description	Records a page load. The path's query and fragment are dropped. id (optional, a UUID) is the page view id that time-spent heartbeats refer to; it is generated when omitted and returned either way. Events are written asynchronously: 202 does not mean the event was stored (bots, prefetches, and visitors who refused consent are dropped silently). With analytics.consent_required on, a granted X-Consent-Token is required (403 analytics.consent_required); without granted consent, events are stored without a link to any subject.
//	@Tags			analytics
//	@Accept			json
//	@Produce		json
//	@Param			X-Consent-Token	header		string					false	"consent subject token"
//	@Param			body			body		requests.IngestPageView	true	"page view"
//	@Success		202				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope	"analytics.consent_required"
//	@Failure		404				{object}	responses.Envelope	"analytics.disabled"
//	@Failure		413				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/analytics/ingest/page-view [post]
func ingestPageViewHandler(ingest analyticsIngestAPI, meta ingestMeta) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.IngestPageView
		if !requests.BindJSON(c, &req) {
			return
		}

		id, err := ingest.PageView(c.Request.Context(), meta.of(c), req.Input())
		respondIngested(c, id, err)
	}
}

// ingestTimeSpentHandler godoc
//
//	@Summary		Record time spent on a page
//	@Description	A heartbeat for page view view_id with the cumulative seconds the page has been in focus (clamped to 4 hours). Send one every 15-30 seconds while the page is visible and one on unload; the highest value is kept. Accepted asynchronously like page views.
//	@Tags			analytics
//	@Accept			json
//	@Produce		json
//	@Param			X-Consent-Token	header		string						false	"consent subject token"
//	@Param			body			body		requests.IngestTimeSpent	true	"heartbeat"
//	@Success		202				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope	"analytics.consent_required"
//	@Failure		404				{object}	responses.Envelope	"analytics.disabled"
//	@Failure		413				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/analytics/ingest/time-spent [post]
func ingestTimeSpentHandler(ingest analyticsIngestAPI, meta ingestMeta) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.IngestTimeSpent
		if !requests.BindJSON(c, &req) {
			return
		}

		id, err := ingest.TimeSpent(c.Request.Context(), meta.of(c), req.Input())
		respondIngested(c, id, err)
	}
}

// ingestNavigationHandler godoc
//
//	@Summary		Record a navigation
//	@Description	A move from one page to another. from is omitted for an entry page; transition is internal, external, back_forward, or direct. Accepted asynchronously like page views.
//	@Tags			analytics
//	@Accept			json
//	@Produce		json
//	@Param			X-Consent-Token	header		string						false	"consent subject token"
//	@Param			body			body		requests.IngestNavigation	true	"navigation"
//	@Success		202				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope	"analytics.consent_required"
//	@Failure		404				{object}	responses.Envelope	"analytics.disabled"
//	@Failure		413				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/analytics/ingest/navigation [post]
func ingestNavigationHandler(ingest analyticsIngestAPI, meta ingestMeta) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.IngestNavigation
		if !requests.BindJSON(c, &req) {
			return
		}

		id, err := ingest.Navigation(c.Request.Context(), meta.of(c), req.Input())
		respondIngested(c, id, err)
	}
}

// ingestSearchHandler godoc
//
//	@Summary		Record a search
//	@Description	A search the visitor ran. The query is lowercased, whitespace-collapsed, and cut to 200 characters. filters may hold category, tag, from, and to. The response id (the client's id when sent) is the search_id to send with result clicks. Accepted asynchronously like page views.
//	@Tags			analytics
//	@Accept			json
//	@Produce		json
//	@Param			X-Consent-Token	header		string					false	"consent subject token"
//	@Param			body			body		requests.IngestSearch	true	"search"
//	@Success		202				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope	"analytics.consent_required"
//	@Failure		404				{object}	responses.Envelope	"analytics.disabled"
//	@Failure		413				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/analytics/ingest/search [post]
func ingestSearchHandler(ingest analyticsIngestAPI, meta ingestMeta) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.IngestSearch
		if !requests.BindJSON(c, &req) {
			return
		}

		id, err := ingest.Search(c.Request.Context(), meta.of(c), req.Input())
		respondIngested(c, id, err)
	}
}

// ingestSearchClickHandler godoc
//
//	@Summary		Record a search result click
//	@Description	A click on result position (from 1) of search search_id, pointing at a post, page, category, or tag. Neither id is checked against stored data, so the endpoint reveals nothing about what exists. Accepted asynchronously like page views.
//	@Tags			analytics
//	@Accept			json
//	@Produce		json
//	@Param			X-Consent-Token	header		string						false	"consent subject token"
//	@Param			body			body		requests.IngestSearchClick	true	"click"
//	@Success		202				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope	"analytics.consent_required"
//	@Failure		404				{object}	responses.Envelope	"analytics.disabled"
//	@Failure		413				{object}	responses.Envelope
//	@Failure		429				{object}	responses.Envelope
//	@Router			/api/v1/analytics/ingest/search-click [post]
func ingestSearchClickHandler(ingest analyticsIngestAPI, meta ingestMeta) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.IngestSearchClick
		if !requests.BindJSON(c, &req) {
			return
		}

		id, err := ingest.SearchClick(c.Request.Context(), meta.of(c), req.Input())
		respondIngested(c, id, err)
	}
}

func respondIngested(c *gin.Context, id uuid.UUID, err error) {
	switch {
	case err == nil:
		c.Header("Cache-Control", "no-store")
		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceAnalytics, responses.CaseAccepted, gin.H{"id": id})
	case errors.Is(err, analyticsdomain.ErrValidation):
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
	default:
		responses.Internal(c, err, "Failed to record analytics event")
	}
}

// ingestMeta derives what the ingest service needs from a request.
type ingestMeta struct {
	// countryHeader names the proxy header with the visitor's country; empty disables it.
	countryHeader string
	// proxies are the peers trusted to set countryHeader.
	proxies []netip.Prefix
}

func newIngestMeta(countryHeader string, trustedProxies []string) ingestMeta {
	meta := ingestMeta{countryHeader: strings.TrimSpace(countryHeader)}

	for _, proxy := range trustedProxies {
		if prefix, err := netip.ParsePrefix(proxy); err == nil {
			meta.proxies = append(meta.proxies, prefix.Masked())
		} else if addr, err := netip.ParseAddr(proxy); err == nil {
			meta.proxies = append(meta.proxies, netip.PrefixFrom(addr.Unmap(), addr.Unmap().BitLen()))
		}
	}

	return meta
}

func (m ingestMeta) of(c *gin.Context) analyticsservice.Meta {
	meta := analyticsservice.Meta{
		Refused:   c.GetBool(contextAnalyticsRefused),
		IP:        c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Country:   m.country(c),
		Prefetch:  isPrefetch(c),
	}

	if subject, ok := c.Get(contextAnalyticsSubject); ok {
		if id, ok := subject.(uuid.UUID); ok {
			meta.Subject = &id
		}
	}

	return meta
}

// country reads the country header only when the request came straight from a trusted proxy;
// anyone else could set it.
func (m ingestMeta) country(c *gin.Context) string {
	if m.countryHeader == "" {
		return ""
	}

	peer, err := netip.ParseAddr(c.RemoteIP())
	if err != nil {
		return ""
	}

	peer = peer.Unmap()
	for _, prefix := range m.proxies {
		if prefix.Contains(peer) {
			return c.GetHeader(m.countryHeader)
		}
	}

	return ""
}

func isPrefetch(c *gin.Context) bool {
	for _, header := range []string{"Sec-Purpose", "Purpose", "X-Moz"} {
		if strings.Contains(strings.ToLower(c.GetHeader(header)), "prefetch") {
			return true
		}
	}

	return false
}

// ingestBodyLimit rejects bodies over max bytes with 413 before they are read.
func ingestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			responses.Failure(c, nethttp.StatusRequestEntityTooLarge, "analytics.payload_too_large", "Request body is too large")
			c.Abort()

			return
		}

		c.Request.Body = nethttp.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// analyticsIngestControllers wires the ingest handlers. The body limit and per-IP rate limit
// run as middleware before the consent gate, so rejected floods never reach the database.
func analyticsIngestControllers(deps Deps, a *routes.Analytics) {
	ingest := deps.AnalyticsIngest
	if ingest == nil {
		return
	}

	a.IngestLimits = gin.HandlersChain{
		ingestBodyLimit(ingestMaxBodyBytes),
		middleware.RateLimit(deps.RateLimiter, deps.Logger, "analytics.ingest", deps.AnalyticsIngestPerMinute, time.Minute),
	}

	meta := newIngestMeta(deps.AnalyticsCountryHeader, deps.TrustedProxies)
	a.PageView = ingestPageViewHandler(ingest, meta)
	a.TimeSpent = ingestTimeSpentHandler(ingest, meta)
	a.Navigation = ingestNavigationHandler(ingest, meta)
	a.Search = ingestSearchHandler(ingest, meta)
	a.SearchClick = ingestSearchClickHandler(ingest, meta)
}

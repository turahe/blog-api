package requests

import (
	"time"

	"github.com/google/uuid"
	analyticsdomain "github.com/turahe/blog-api/internal/core/analytics/domain"
	analyticsservice "github.com/turahe/blog-api/internal/core/analytics/service"
)

// IngestPageView is POST /api/v1/analytics/ingest/page-view. id is the client's page view id
// (generated when omitted) that time-spent heartbeats refer to; session_id is the client's
// short-lived session.
type IngestPageView struct {
	ID        string `json:"id"         binding:"omitempty,uuid"`
	SessionID string `json:"session_id" binding:"required,uuid"`
	Path      string `json:"path"       binding:"required,max=2048"`
	Referrer  string `json:"referrer"   binding:"max=2048"`
}

// Input converts the request for the ingest service.
func (r IngestPageView) Input() analyticsservice.PageViewInput {
	return analyticsservice.PageViewInput{
		ID: optionalUUID(r.ID), SessionID: optionalUUID(r.SessionID), Path: r.Path, Referrer: r.Referrer,
	}
}

// IngestTimeSpent is POST /api/v1/analytics/ingest/time-spent: a heartbeat with the
// cumulative seconds page view view_id has been in focus.
type IngestTimeSpent struct {
	ViewID       string `json:"view_id"       binding:"required,uuid"`
	SessionID    string `json:"session_id"    binding:"required,uuid"`
	Path         string `json:"path"          binding:"required,max=2048"`
	FocusSeconds int    `json:"focus_seconds" binding:"min=0"`
}

// Input converts the request for the ingest service.
func (r IngestTimeSpent) Input() analyticsservice.TimeSpentInput {
	return analyticsservice.TimeSpentInput{
		ViewID: optionalUUID(r.ViewID), SessionID: optionalUUID(r.SessionID), Path: r.Path, FocusSeconds: r.FocusSeconds,
	}
}

// IngestNavigation is POST /api/v1/analytics/ingest/navigation. from is omitted for an entry.
type IngestNavigation struct {
	ID         string `json:"id"         binding:"omitempty,uuid"`
	SessionID  string `json:"session_id" binding:"required,uuid"`
	From       string `json:"from"       binding:"max=2048"`
	To         string `json:"to"         binding:"required,max=2048"`
	Transition string `json:"transition" binding:"required,oneof=internal external back_forward direct"`
}

// Input converts the request for the ingest service.
func (r IngestNavigation) Input() analyticsservice.NavigationInput {
	return analyticsservice.NavigationInput{
		ID: optionalUUID(r.ID), SessionID: optionalUUID(r.SessionID), From: r.From, To: r.To, Transition: r.Transition,
	}
}

// IngestSearch is POST /api/v1/analytics/ingest/search. id is the search id clicks refer to
// (generated when omitted). filters keys: category, tag, from, to.
type IngestSearch struct {
	ID          string            `json:"id"           binding:"omitempty,uuid"`
	SessionID   string            `json:"session_id"   binding:"required,uuid"`
	Query       string            `json:"query"        binding:"required,max=1000"`
	ResultCount int               `json:"result_count" binding:"min=0"`
	Filters     map[string]string `json:"filters"      binding:"max=4"`
}

// Input converts the request for the ingest service.
func (r IngestSearch) Input() analyticsservice.SearchInput {
	return analyticsservice.SearchInput{
		ID: optionalUUID(r.ID), SessionID: optionalUUID(r.SessionID), Query: r.Query,
		ResultCount: r.ResultCount, Filters: r.Filters,
	}
}

// IngestSearchClick is POST /api/v1/analytics/ingest/search-click: result position (from 1)
// of search search_id was clicked.
type IngestSearchClick struct {
	ID           string `json:"id"            binding:"omitempty,uuid"`
	SessionID    string `json:"session_id"    binding:"required,uuid"`
	SearchID     string `json:"search_id"     binding:"required,uuid"`
	Position     int    `json:"position"      binding:"required,min=1"`
	ResourceType string `json:"resource_type" binding:"required,oneof=post page category tag"`
	ResourceID   string `json:"resource_id"   binding:"required,uuid"`
}

// Input converts the request for the ingest service.
func (r IngestSearchClick) Input() analyticsservice.SearchClickInput {
	return analyticsservice.SearchClickInput{
		ID: optionalUUID(r.ID), SessionID: optionalUUID(r.SessionID), SearchID: optionalUUID(r.SearchID),
		Position: r.Position, ResourceType: r.ResourceType, ResourceID: optionalUUID(r.ResourceID),
	}
}

// AnalyticsReport is the query of the admin analytics reports. from and to are inclusive dates
// (YYYY-MM-DD) in the site time zone; the default is the last 30 days through today. grain is
// picked from the range length when omitted; compare defaults to previous.
type AnalyticsReport struct {
	From    string `form:"from"    binding:"omitempty,datetime=2006-01-02"`
	To      string `form:"to"      binding:"omitempty,datetime=2006-01-02"`
	Grain   string `form:"grain"   binding:"omitempty,oneof=day week month"`
	Compare string `form:"compare" binding:"omitempty,oneof=previous none"`
	Limit   int    `form:"limit"   binding:"omitempty,min=1,max=100"`
	Sort    string `form:"sort"    binding:"omitempty,oneof=views time rising"`
}

// Query converts the request for the reports service.
func (r AnalyticsReport) Query() analyticsdomain.ReportQuery {
	return analyticsdomain.ReportQuery{
		From: optionalDate(r.From), To: optionalDate(r.To), Grain: analyticsdomain.Grain(r.Grain),
		Compare: r.Compare != "none", Limit: r.Limit, Sort: r.Sort,
	}
}

// AnalyticsExport is POST /api/v1/admin/analytics/export: the rollups of from through to (dates
// in the site time zone, widened to whole periods of grain). grain is picked from the range
// length when omitted. current_password, and two_factor_code when two-factor is enabled,
// re-verify the caller.
type AnalyticsExport struct {
	From            string `json:"from"             binding:"required,datetime=2006-01-02" example:"2026-08-01"`
	To              string `json:"to"               binding:"required,datetime=2006-01-02" example:"2026-08-31"`
	Grain           string `json:"grain"            binding:"omitempty,oneof=day week month" example:"day"`
	CurrentPassword string `json:"current_password" binding:"required,max=128"`
	TwoFactorCode   string `json:"two_factor_code"  binding:"max=32" example:"123456"`
}

// Input converts the request for the exports service.
func (r AnalyticsExport) Input() analyticsservice.ExportRequest {
	return analyticsservice.ExportRequest{
		Query:    analyticsdomain.ReportQuery{From: optionalDate(r.From), To: optionalDate(r.To), Grain: analyticsdomain.Grain(r.Grain)},
		Password: r.CurrentPassword, Code: r.TwoFactorCode,
	}
}

// optionalDate parses a validated YYYY-MM-DD field; empty is the zero time.
func optionalDate(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}
	}

	return t
}

// optionalUUID parses a validated uuid field; empty (or invalid) is uuid.Nil.
func optionalUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}

	return id
}

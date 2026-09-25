package requests

import (
	"github.com/google/uuid"
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

// optionalUUID parses a validated uuid field; empty (or invalid) is uuid.Nil.
func optionalUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}

	return id
}

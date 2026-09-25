// Package domain defines analytics events and the rules that normalise them before storage.
package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrValidation marks an event the client must fix.
var ErrValidation = errors.New("analytics: validation failed")

// Kind names an event type; each kind has its own table.
type Kind string

// Event kinds.
const (
	KindPageView    Kind = "page_view"
	KindTimeSpent   Kind = "time_spent"
	KindNavigation  Kind = "navigation"
	KindSearch      Kind = "search"
	KindSearchClick Kind = "search_click"
)

// Visitor identifies who produced an event without storing an IP address or user agent.
type Visitor struct {
	// SubjectUUID is the consent subject when it granted analytics; nil for anonymous events.
	SubjectUUID *uuid.UUID
	// Hash counts unique visitors: stable for a subject, per day otherwise.
	Hash string
	// SessionID is the client's short-lived session.
	SessionID uuid.UUID
}

// Event is one normalised analytics event. Exactly one of the kind-specific fields is set.
type Event struct {
	Kind       Kind
	UUID       uuid.UUID
	Visitor    Visitor
	OccurredAt time.Time

	PageView    *PageView
	TimeSpent   *TimeSpent
	Navigation  *Navigation
	Search      *Search
	SearchClick *SearchClick
}

// PageView is one page load.
type PageView struct {
	Path     string
	Referrer string // empty when absent
	Country  string // ISO 3166-1 alpha-2, empty when unknown
	Device   Device
	Browser  string
}

// TimeSpent is a heartbeat for a page view (the event UUID is the page view's). FocusSeconds
// is the cumulative time the page was in focus.
type TimeSpent struct {
	Path         string
	FocusSeconds int
}

// Navigation is a move between pages. From is empty for an entry.
type Navigation struct {
	From       string
	To         string
	Transition Transition
}

// Search is one search the visitor ran.
type Search struct {
	Query       string
	ResultCount int
	Filters     map[string]string
}

// SearchClick is a click on a search result.
type SearchClick struct {
	SearchUUID   uuid.UUID
	Position     int
	ResourceType ResourceType
	ResourceUUID uuid.UUID
}

// Device is a coarse device class.
type Device string

// Device classes.
const (
	DeviceDesktop Device = "desktop"
	DeviceTablet  Device = "tablet"
	DeviceMobile  Device = "mobile"
)

// Transition is how the visitor reached a page.
type Transition string

// Transitions.
const (
	TransitionInternal    Transition = "internal"
	TransitionExternal    Transition = "external"
	TransitionBackForward Transition = "back_forward"
	TransitionDirect      Transition = "direct"
)

// ResourceType is what a search result points at.
type ResourceType string

// Resource types.
const (
	ResourcePost     ResourceType = "post"
	ResourcePage     ResourceType = "page"
	ResourceCategory ResourceType = "category"
	ResourceTag      ResourceType = "tag"
)

// ValidTransition reports whether t is a known transition.
func ValidTransition(t Transition) bool {
	switch t {
	case TransitionInternal, TransitionExternal, TransitionBackForward, TransitionDirect:
		return true
	default:
		return false
	}
}

// ValidResourceType reports whether r is a known resource type.
func ValidResourceType(r ResourceType) bool {
	switch r {
	case ResourcePost, ResourcePage, ResourceCategory, ResourceTag:
		return true
	default:
		return false
	}
}

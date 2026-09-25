// Package service validates analytics events and writes them in the background.
package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/analytics/domain"
	"github.com/turahe/blog-api/internal/core/analytics/ports"
)

// IDGenerator creates event ids the client did not supply.
type IDGenerator interface{ New() uuid.UUID }

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// Meta is what the transport knows about the request that carried an event.
type Meta struct {
	// Subject is the consent subject when it granted analytics.
	Subject *uuid.UUID
	// Refused is set when the subject refused or withdrew analytics: the event is dropped.
	Refused bool
	// IP and UserAgent key the anonymous visitor hash; neither is stored.
	IP        string
	UserAgent string
	// Country is an ISO 3166-1 alpha-2 code from a trusted proxy, if any.
	Country string
	// Prefetch marks a speculative load the visitor may never see: the event is dropped.
	Prefetch bool
}

// PageViewInput is a page load. ID is the client's page view id (generated when zero); later
// time-spent heartbeats refer to it.
type PageViewInput struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	Path      string
	Referrer  string
}

// TimeSpentInput is a heartbeat for page view ViewID with the cumulative focus time.
type TimeSpentInput struct {
	ViewID       uuid.UUID
	SessionID    uuid.UUID
	Path         string
	FocusSeconds int
}

// NavigationInput is a move from From (empty for an entry) to To.
type NavigationInput struct {
	ID         uuid.UUID
	SessionID  uuid.UUID
	From       string
	To         string
	Transition string
}

// SearchInput is a search. ID is the search id clicks refer to (generated when zero).
type SearchInput struct {
	ID          uuid.UUID
	SessionID   uuid.UUID
	Query       string
	ResultCount int
	Filters     map[string]string
}

// SearchClickInput is a click on result Position of search SearchID.
type SearchClickInput struct {
	ID           uuid.UUID
	SessionID    uuid.UUID
	SearchID     uuid.UUID
	Position     int
	ResourceType string
	ResourceID   uuid.UUID
}

// Ingest validates and normalises events and hands them to the sink. It never waits for
// storage, and it answers the same way whether or not an event is kept, so the endpoints
// reveal nothing about content, users, or consent.
type Ingest struct {
	sink   ports.Sink
	live   ports.LiveSink
	hasher ports.IdentityHasher
	ids    IDGenerator
	clock  Clock
}

// NewIngest returns an Ingest. Without a hasher, visitor hashes are keyed with a random
// per-process key, so unique counts across API replicas are approximate.
func NewIngest(sink ports.Sink, hasher ports.IdentityHasher, ids IDGenerator, clock Clock) *Ingest {
	if hasher == nil {
		hasher = newProcessHasher()
	}

	return &Ingest{sink: sink, hasher: hasher, ids: ids, clock: clock}
}

// WithLive also announces every accepted event to the live view.
func (s *Ingest) WithLive(live ports.LiveSink) *Ingest {
	s.live = live

	return s
}

// PageView records a page load and returns its id.
func (s *Ingest) PageView(_ context.Context, meta Meta, in PageViewInput) (uuid.UUID, error) {
	path, err := domain.NormalizePath(in.Path)
	if err != nil {
		return uuid.Nil, err
	}

	agent := domain.ClassifyAgent(meta.UserAgent)

	return s.accept(meta, agent, in.ID, in.SessionID, domain.Event{
		Kind: domain.KindPageView,
		PageView: &domain.PageView{
			Path: path, Referrer: domain.NormalizeReferrer(in.Referrer), Country: domain.NormalizeCountry(meta.Country),
			Device: agent.Device, Browser: agent.Browser,
		},
	})
}

// TimeSpent records a heartbeat for a page view and returns the page view id.
func (s *Ingest) TimeSpent(_ context.Context, meta Meta, in TimeSpentInput) (uuid.UUID, error) {
	if in.ViewID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: view_id is required", domain.ErrValidation)
	}

	path, err := domain.NormalizePath(in.Path)
	if err != nil {
		return uuid.Nil, err
	}

	return s.accept(meta, domain.ClassifyAgent(meta.UserAgent), in.ViewID, in.SessionID, domain.Event{
		Kind:      domain.KindTimeSpent,
		TimeSpent: &domain.TimeSpent{Path: path, FocusSeconds: domain.ClampFocus(in.FocusSeconds)},
	})
}

// Navigation records a move between pages and returns its id.
func (s *Ingest) Navigation(_ context.Context, meta Meta, in NavigationInput) (uuid.UUID, error) {
	to, err := domain.NormalizePath(in.To)
	if err != nil {
		return uuid.Nil, err
	}

	var from string
	if in.From != "" {
		if from, err = domain.NormalizePath(in.From); err != nil {
			return uuid.Nil, err
		}
	}

	transition := domain.Transition(in.Transition)
	if !domain.ValidTransition(transition) {
		return uuid.Nil, fmt.Errorf("%w: unknown transition %q", domain.ErrValidation, in.Transition)
	}

	return s.accept(meta, domain.ClassifyAgent(meta.UserAgent), in.ID, in.SessionID, domain.Event{
		Kind:       domain.KindNavigation,
		Navigation: &domain.Navigation{From: from, To: to, Transition: transition},
	})
}

// Search records a search and returns the id its clicks refer to.
func (s *Ingest) Search(_ context.Context, meta Meta, in SearchInput) (uuid.UUID, error) {
	query, err := domain.NormalizeQuery(in.Query)
	if err != nil {
		return uuid.Nil, err
	}

	if in.ResultCount < 0 {
		return uuid.Nil, fmt.Errorf("%w: result_count must not be negative", domain.ErrValidation)
	}

	filters, err := domain.NormalizeFilters(in.Filters)
	if err != nil {
		return uuid.Nil, err
	}

	return s.accept(meta, domain.ClassifyAgent(meta.UserAgent), in.ID, in.SessionID, domain.Event{
		Kind: domain.KindSearch,
		Search: &domain.Search{
			Query: query, ResultCount: min(in.ResultCount, domain.MaxResultCount), Filters: filters,
		},
	})
}

// SearchClick records a click on a search result and returns its id.
func (s *Ingest) SearchClick(_ context.Context, meta Meta, in SearchClickInput) (uuid.UUID, error) {
	if in.SearchID == uuid.Nil || in.ResourceID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: search_id and resource_id are required", domain.ErrValidation)
	}

	if in.Position < 1 {
		return uuid.Nil, fmt.Errorf("%w: position starts at 1", domain.ErrValidation)
	}

	resource := domain.ResourceType(in.ResourceType)
	if !domain.ValidResourceType(resource) {
		return uuid.Nil, fmt.Errorf("%w: unknown resource_type %q", domain.ErrValidation, in.ResourceType)
	}

	return s.accept(meta, domain.ClassifyAgent(meta.UserAgent), in.ID, in.SessionID, domain.Event{
		Kind: domain.KindSearchClick,
		SearchClick: &domain.SearchClick{
			SearchUUID: in.SearchID, Position: min(in.Position, domain.MaxPosition),
			ResourceType: resource, ResourceUUID: in.ResourceID,
		},
	})
}

// accept completes event and queues it, unless it comes from a bot, a prefetch, or a
// subject that refused analytics. Either way the caller gets the same answer.
func (s *Ingest) accept(meta Meta, agent domain.Agent, id, session uuid.UUID, event domain.Event) (uuid.UUID, error) {
	if session == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: session_id is required", domain.ErrValidation)
	}

	if id == uuid.Nil {
		id = s.ids.New()
	}

	if meta.Refused || meta.Prefetch || agent.Bot {
		return id, nil
	}

	now := s.clock.Now().UTC()
	event.UUID, event.OccurredAt = id, now
	event.Visitor = domain.Visitor{SubjectUUID: meta.Subject, Hash: s.visitorHash(meta, now), SessionID: session}

	s.sink.Enqueue(event)

	if s.live != nil {
		s.live.Publish(domain.LiveEventOf(event))
	}

	return id, nil
}

// visitorHash is stable for a consent subject. For anyone else it keys the IP and user agent
// with the day, so the same visitor gets an unrelated hash tomorrow.
func (s *Ingest) visitorHash(meta Meta, at time.Time) string {
	if meta.Subject != nil {
		return s.hasher.MAC("analytics:subject:" + meta.Subject.String())
	}

	return s.hasher.MAC("analytics:visitor:" + at.Format(time.DateOnly) + ":" + meta.IP + ":" + meta.UserAgent)
}

// processHasher keys hashes with a random key that lives only in this process.
type processHasher struct{ key []byte }

func newProcessHasher() processHasher {
	key := make([]byte, sha256.Size)
	_, _ = rand.Read(key)

	return processHasher{key: key}
}

func (h processHasher) MAC(value string) string {
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte(value))

	return hex.EncodeToString(mac.Sum(nil))
}

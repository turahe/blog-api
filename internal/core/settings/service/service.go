// Package service implements reading, validating, and updating admin settings.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/readcache"
	settingsdomain "github.com/turahe/blog-api/internal/core/settings/domain"
	"github.com/turahe/blog-api/internal/core/settings/ports"
)

// Limits on one request.
const (
	MaxUpdates     = 100
	maxKeyLength   = 128
	defaultPerPage = 20
	maxPerPage     = 100
)

// ErrValidation rejects malformed filters and empty or oversized update batches.
var ErrValidation = errors.New("validation error")

// AggregateID identifies the site-wide settings aggregate in events.
var AggregateID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("urn:blog-api:settings"))

// IDGenerator returns new UUIDs.
type IDGenerator interface {
	New() uuid.UUID
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Service implements the settings use cases.
type Service struct {
	repo      ports.Repository
	catalogue settingsdomain.Catalogue
	ids       IDGenerator
	clock     Clock
	unit      event.Unit
	cache     readcache.Cache
}

// New returns a Service over catalogue.
func New(repo ports.Repository, catalogue settingsdomain.Catalogue, ids IDGenerator, clock Clock) *Service {
	return &Service{repo: repo, catalogue: catalogue, ids: ids, clock: clock}
}

// WithEvents runs updates in unit's transaction and records settings.updated there.
func (s *Service) WithEvents(unit event.Unit) *Service {
	s.unit = unit
	return s
}

// WithCache caches the stored values; every applied update invalidates them.
func (s *Service) WithCache(cache readcache.Cache) *Service {
	s.cache = cache
	return s
}

// ListFilter selects settings for display.
type ListFilter struct {
	// Category limits the result to one category; empty returns all.
	Category settingsdomain.Category
	// IncludeAdminOnly adds admin_only keys. server_only keys are never listed.
	IncludeAdminOnly bool
}

// List returns the catalogue resolved against storage, filtered by sensitivity.
func (s *Service) List(ctx context.Context, filter ListFilter) ([]settingsdomain.Setting, error) {
	if filter.Category != "" && !settingsdomain.ValidCategory(filter.Category) {
		return nil, fmt.Errorf("%w: unknown category", ErrValidation)
	}

	stored, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}

	out := []settingsdomain.Setting{}

	for _, def := range s.catalogue.Definitions() {
		switch {
		case def.Sensitivity == settingsdomain.ServerOnly:
			continue
		case def.Sensitivity == settingsdomain.AdminOnly && !filter.IncludeAdminOnly:
			continue
		case filter.Category != "" && def.Category != filter.Category:
			continue
		}

		out = append(out, resolve(def, stored))
	}

	return out, nil
}

// Values returns every key's effective value, including server_only keys, for services
// that read settings.
func (s *Service) Values(ctx context.Context) (settingsdomain.Values, error) {
	stored, err := s.stored(ctx)
	if err != nil {
		return settingsdomain.Values{}, err
	}

	values := map[string]any{}
	for _, def := range s.catalogue.Definitions() {
		values[def.Key] = resolve(def, stored).Value
	}

	return settingsdomain.NewValues(values), nil
}

// Update is one submitted key. Version, when set, must match the key's current version.
type Update struct {
	Key     string
	Value   json.RawMessage
	Version *int64
}

// Actor identifies who changed settings, for history and events.
type Actor struct {
	UserID    *uuid.UUID
	RequestID string
}

// Result lists applied keys and keys whose submitted value equals the current one.
type Result struct {
	Applied   []settingsdomain.Change
	Unchanged []string
}

type decodedUpdate struct {
	def     settingsdomain.Definition
	value   any
	version *int64
}

// Update validates every submitted key, then applies the changed ones atomically. Any
// invalid key rejects the whole request with a *domain.ValidationError; a stale version
// returns domain.ErrVersionConflict and applies nothing.
func (s *Service) Update(ctx context.Context, actor Actor, updates []Update) (Result, error) {
	decoded, err := s.validate(ctx, updates)
	if err != nil {
		return Result{}, err
	}

	var result Result

	now := s.clock.Now().UTC()

	err = s.unit.InTx(ctx, func(ctx context.Context) error {
		result = Result{Applied: []settingsdomain.Change{}, Unchanged: []string{}}

		return s.apply(ctx, actor, decoded, now, &result)
	})
	if err != nil {
		return Result{}, err
	}

	if len(result.Applied) > 0 {
		readcache.Invalidate(ctx, s.cache, readcache.Settings)
	}

	for _, change := range result.Applied {
		audit.AddChange(ctx, change.Key, change.Previous, change.New)
	}

	return result, nil
}

func (s *Service) apply(ctx context.Context, actor Actor, updates []decodedUpdate, now time.Time, result *Result) error {
	keys := make([]string, 0, len(updates))
	for _, u := range updates {
		keys = append(keys, u.def.Key)
	}

	locked, err := s.repo.Lock(ctx, keys)
	if err != nil {
		return err
	}

	current := byKey(locked)

	for _, u := range updates {
		prev := resolve(u.def, current)
		if u.version != nil && *u.version != prev.Version {
			return fmt.Errorf("%w: %s is at version %d", settingsdomain.ErrVersionConflict, u.def.Key, prev.Version)
		}

		// A stored value that no longer passes the catalogue rules is replaced even when
		// the submitted value equals the default it currently resolves to.
		storedInvalid := prev.Defaulted && prev.Version > 0
		if settingsdomain.Equal(prev.Value, u.value) && !storedInvalid {
			result.Unchanged = append(result.Unchanged, u.def.Key)
			continue
		}

		if err := s.save(ctx, actor, u, prev, now); err != nil {
			return err
		}

		result.Applied = append(result.Applied, settingsdomain.Change{
			Key: u.def.Key, Previous: prev.Value, New: u.value,
			Version: prev.Version + 1, Sensitivity: u.def.Sensitivity,
		})
	}

	if len(result.Applied) == 0 {
		return nil
	}

	return s.unit.Record(ctx, updatedEvent(actor, result.Applied, now))
}

func (s *Service) save(ctx context.Context, actor Actor, u decodedUpdate, prev settingsdomain.Setting, now time.Time) error {
	previous, err := json.Marshal(prev.Value)
	if err != nil {
		return fmt.Errorf("encode previous %s: %w", u.def.Key, err)
	}

	value, err := json.Marshal(u.value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", u.def.Key, err)
	}

	return s.repo.Save(ctx, settingsdomain.Write{
		Key:             u.def.Key,
		Previous:        previous,
		Value:           value,
		ExpectedVersion: prev.Version,
		ChangedBy:       actor.UserID,
		RequestID:       actor.RequestID,
		HistoryID:       s.ids.New(),
		At:              now,
	})
}

// validate decodes every update and collects all violations before rejecting any.
func (s *Service) validate(ctx context.Context, updates []Update) ([]decodedUpdate, error) {
	if len(updates) == 0 {
		return nil, fmt.Errorf("%w: updates required", ErrValidation)
	}

	if len(updates) > MaxUpdates {
		return nil, fmt.Errorf("%w: at most %d updates per request", ErrValidation, MaxUpdates)
	}

	var violations []settingsdomain.Violation

	out := make([]decodedUpdate, 0, len(updates))
	seen := make(map[string]struct{}, len(updates))
	keys := make([]string, 0, len(updates))

	for _, u := range updates {
		key := strings.TrimSpace(u.Key)
		keys = append(keys, key)

		if v := s.checkKey(key, seen); v != nil {
			violations = append(violations, *v)
			continue
		}

		def, _ := s.catalogue.Lookup(key)

		value, v := def.Decode(u.Value)
		if v != nil {
			violations = append(violations, *v)
			continue
		}

		out = append(out, decodedUpdate{def: def, value: value, version: u.Version})
	}

	audit.AddMetadata(ctx, "keys", keys)

	if len(violations) > 0 {
		audit.AddMetadata(ctx, "violations", violations)
		return nil, &settingsdomain.ValidationError{Violations: violations}
	}

	return out, nil
}

func (s *Service) checkKey(key string, seen map[string]struct{}) *settingsdomain.Violation {
	def, ok := s.catalogue.Lookup(key)
	if !ok {
		return &settingsdomain.Violation{Key: truncate(key), Reason: settingsdomain.ReasonUnknownKey, Message: "unknown setting"}
	}

	if _, dup := seen[key]; dup {
		return &settingsdomain.Violation{Key: key, Reason: settingsdomain.ReasonDuplicate, Message: "key submitted more than once"}
	}

	seen[key] = struct{}{}

	if def.Sensitivity == settingsdomain.ServerOnly {
		return &settingsdomain.Violation{Key: key, Reason: settingsdomain.ReasonReadOnly, Message: "setting cannot be changed over the API"}
	}

	return nil
}

// History returns recorded changes, newest first. Values of keys that are no longer in
// the catalogue or are server_only are redacted.
func (s *Service) History(ctx context.Context, filter settingsdomain.HistoryFilter) (settingsdomain.HistoryPage, error) {
	filter.Key = strings.TrimSpace(filter.Key)
	if len(filter.Key) > maxKeyLength {
		return settingsdomain.HistoryPage{}, fmt.Errorf("%w: key too long", ErrValidation)
	}

	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.PerPage < 1 {
		filter.PerPage = defaultPerPage
	}

	filter.PerPage = min(filter.PerPage, maxPerPage)

	page, err := s.repo.History(ctx, filter)
	if err != nil {
		return settingsdomain.HistoryPage{}, err
	}

	for i, entry := range page.Items {
		if def, ok := s.catalogue.Lookup(entry.Key); !ok || def.Sensitivity == settingsdomain.ServerOnly {
			page.Items[i].Previous, page.Items[i].New, page.Items[i].Redacted = nil, nil, true
		}
	}

	return page, nil
}

func (s *Service) stored(ctx context.Context) (map[string]settingsdomain.Stored, error) {
	rows, err := readcache.Through(ctx, s.cache, readcache.Settings, readcache.Key("stored"), func() ([]settingsdomain.Stored, error) {
		return s.repo.List(ctx)
	})
	if err != nil {
		return nil, err
	}

	return byKey(rows), nil
}

// resolve returns def's stored value, or its default when no valid value is stored. A
// stored value that fails the current rules keeps its version so the next write succeeds.
func resolve(def settingsdomain.Definition, stored map[string]settingsdomain.Stored) settingsdomain.Setting {
	row, ok := stored[def.Key]
	if ok {
		if value, v := def.Decode(row.Value); v == nil {
			updatedAt := row.UpdatedAt

			return settingsdomain.Setting{
				Definition: def, Value: value, Version: row.Version,
				UpdatedAt: &updatedAt, UpdatedBy: row.UpdatedBy,
			}
		}
	}

	value := def.Default
	if list, isList := value.([]string); isList {
		value = slices.Clone(list)
	}

	return settingsdomain.Setting{Definition: def, Value: value, Version: row.Version, Defaulted: true}
}

func byKey(rows []settingsdomain.Stored) map[string]settingsdomain.Stored {
	out := make(map[string]settingsdomain.Stored, len(rows))
	for _, row := range rows {
		out[row.Key] = row
	}

	return out
}

func truncate(key string) string {
	if len(key) > maxKeyLength {
		return key[:maxKeyLength]
	}

	return key
}

type changedKey struct {
	Key           string `json:"key"`
	PreviousValue any    `json:"previous_value"`
	NewValue      any    `json:"new_value"`
	Sensitivity   string `json:"sensitivity"`
}

type updatedPayload struct {
	ChangedKeys []changedKey `json:"changed_keys"`
	ActorID     *uuid.UUID   `json:"actor_id,omitempty"`
	RequestID   string       `json:"request_id,omitempty"`
}

func updatedEvent(actor Actor, changes []settingsdomain.Change, at time.Time) event.Event {
	payload := updatedPayload{ChangedKeys: make([]changedKey, 0, len(changes)), ActorID: actor.UserID, RequestID: actor.RequestID}
	for _, change := range changes {
		payload.ChangedKeys = append(payload.ChangedKeys, changedKey{
			Key: change.Key, PreviousValue: change.Previous, NewValue: change.New, Sensitivity: string(change.Sensitivity),
		})
	}

	return event.New(event.SettingsUpdated, event.AggregateSettings, AggregateID, actor.UserID, at, payload)
}

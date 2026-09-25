// Package service records analytics consent and answers whether a subject may be measured.
package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/consent/domain"
	"github.com/turahe/blog-api/internal/core/consent/ports"
	"github.com/turahe/blog-api/internal/core/event"
)

// IDGenerator creates public ids.
type IDGenerator interface{ New() uuid.UUID }

// Clock returns the current time.
type Clock interface{ Now() time.Time }

// Service manages consent subjects and decisions.
type Service struct {
	repo   ports.Repository
	ids    IDGenerator
	clock  Clock
	events event.Unit
}

// New returns a Service.
func New(repo ports.Repository, ids IDGenerator, clock Clock) *Service {
	return &Service{repo: repo, ids: ids, clock: clock}
}

// WithEvents records analytics.consent.* events in the outbox.
func (s *Service) WithEvents(events event.Unit) *Service {
	s.events = events
	return s
}

// State is a subject with its consents. Token is set only when the subject was just
// created: it is the client's only copy.
type State struct {
	Subject  domain.Subject
	Token    string
	Consents []domain.Consent
}

// Store records decisions for the subject identified by token, creating a subject (and a
// new token) when token is empty or unknown. userID is the signed-in user, if any:
// granting authenticated analytics links the subject to them, and refusing it unlinks.
func (s *Service) Store(
	ctx context.Context, token string, userID *uuid.UUID, decisions map[domain.Purpose]bool, policyVersion string,
) (State, error) {
	if err := validateDecisions(decisions, policyVersion, userID); err != nil {
		return State{}, err
	}

	var state State

	err := s.events.InTx(ctx, func(ctx context.Context) error {
		subject, newToken, err := s.subject(ctx, token)
		if err != nil {
			return err
		}

		state = State{Subject: subject, Token: newToken}

		current, err := s.repo.List(ctx, subject.UUID)
		if err != nil {
			return err
		}

		state.Consents, err = s.apply(ctx, subject, current, decisions, policyVersion)
		if err != nil {
			return err
		}

		if linked, ok := decisions[domain.PurposeAuthenticatedAnalytics]; ok {
			state.Subject.UserUUID = nil
			if linked {
				state.Subject.UserUUID = userID
			}

			return s.repo.SetUser(ctx, subject.UUID, state.Subject.UserUUID)
		}

		return nil
	})
	if err != nil {
		return State{}, err
	}

	return state, nil
}

// subject returns the subject for token, or creates one with a fresh token.
func (s *Service) subject(ctx context.Context, token string) (domain.Subject, string, error) {
	if token != "" {
		subject, err := s.repo.SubjectByToken(ctx, domain.HashToken(token))
		if err == nil {
			return subject, "", s.repo.Touch(ctx, subject.UUID, s.clock.Now())
		}

		if !errors.Is(err, domain.ErrNotFound) {
			return domain.Subject{}, "", err
		}
	}

	newToken, hash, err := domain.NewToken()
	if err != nil {
		return domain.Subject{}, "", fmt.Errorf("generate consent token: %w", err)
	}

	now := s.clock.Now()
	subject := domain.Subject{UUID: s.ids.New(), CreatedAt: now, LastSeenAt: now}

	if err := s.repo.CreateSubject(ctx, subject, hash); err != nil {
		return domain.Subject{}, "", err
	}

	return subject, newToken, nil
}

// apply saves each decision that changes the stored consent and records its event. It
// returns the subject's consents after the change, in purpose order.
func (s *Service) apply(
	ctx context.Context, subject domain.Subject, current []domain.Consent, decisions map[domain.Purpose]bool, version string,
) ([]domain.Consent, error) {
	byPurpose := map[domain.Purpose]domain.Consent{}
	for _, c := range current {
		byPurpose[c.Purpose] = c
	}

	now := s.clock.Now()

	for _, purpose := range domain.Purposes {
		granted, ok := decisions[purpose]
		if !ok {
			continue
		}

		before, exists := byPurpose[purpose]
		if !exists {
			before = domain.Consent{UUID: s.ids.New(), SubjectUUID: subject.UUID, Purpose: purpose}
		}

		after := before.Decide(granted, version, now)
		if exists && after.Status == before.Status && after.PolicyVersion == before.PolicyVersion {
			continue
		}

		if err := s.save(ctx, after); err != nil {
			return nil, err
		}

		byPurpose[purpose] = after
	}

	out := make([]domain.Consent, 0, len(byPurpose))
	for _, purpose := range domain.Purposes {
		if c, ok := byPurpose[purpose]; ok {
			out = append(out, c)
		}
	}

	return out, nil
}

// save stores c and records its event. Withdrawing analytics also deletes the analytics events
// already stored under the subject.
func (s *Service) save(ctx context.Context, c domain.Consent) error {
	if err := s.repo.Save(ctx, c); err != nil {
		return err
	}

	if c.Purpose == domain.PurposeAnalytics && c.Status == domain.StatusWithdrawn {
		if err := s.repo.DeleteSubjectEvents(ctx, c.SubjectUUID); err != nil {
			return err
		}
	}

	return s.events.Record(ctx, consentEvent(c))
}

// Current returns the consents of the subject identified by token.
func (s *Service) Current(ctx context.Context, token string) (State, error) {
	if token == "" {
		return State{}, domain.ErrNotFound
	}

	subject, err := s.repo.SubjectByToken(ctx, domain.HashToken(token))
	if err != nil {
		return State{}, err
	}

	if err := s.repo.Touch(ctx, subject.UUID, s.clock.Now()); err != nil {
		return State{}, err
	}

	consents, err := s.repo.List(ctx, subject.UUID)
	if err != nil {
		return State{}, err
	}

	return State{Subject: subject, Consents: consents}, nil
}

// Withdraw revokes a consent. The caller proves ownership with the subject's token or as
// the user the subject is linked to; otherwise the consent is ErrNotFound. Withdrawing
// analytics also withdraws authenticated analytics and unlinks the user.
func (s *Service) Withdraw(ctx context.Context, token string, userID *uuid.UUID, consentID uuid.UUID) (domain.Consent, error) {
	consent, subject, err := s.repo.Get(ctx, consentID)
	if err != nil {
		return domain.Consent{}, err
	}

	if !s.owns(ctx, subject, token, userID) {
		return domain.Consent{}, domain.ErrNotFound
	}

	err = s.events.InTx(ctx, func(ctx context.Context) error {
		purposes := []domain.Purpose{consent.Purpose}
		if consent.Purpose == domain.PurposeAnalytics {
			purposes = domain.Purposes
		}

		current, err := s.repo.List(ctx, subject.UUID)
		if err != nil {
			return err
		}

		for _, c := range current {
			if !c.Granted() || !slices.Contains(purposes, c.Purpose) {
				continue
			}

			withdrawn := c.Decide(false, c.PolicyVersion, s.clock.Now())
			if err := s.save(ctx, withdrawn); err != nil {
				return err
			}

			if c.UUID == consent.UUID {
				consent = withdrawn
			}
		}

		if slices.Contains(purposes, domain.PurposeAuthenticatedAnalytics) && subject.UserUUID != nil {
			return s.repo.SetUser(ctx, subject.UUID, nil)
		}

		return nil
	})
	if err != nil {
		return domain.Consent{}, err
	}

	return consent, nil
}

// Allowed reports whether the subject identified by token has granted purpose.
func (s *Service) Allowed(ctx context.Context, token string, purpose domain.Purpose) (bool, error) {
	state, err := s.Current(ctx, token)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	for _, c := range state.Consents {
		if c.Purpose == purpose {
			return c.Granted(), nil
		}
	}

	return false, nil
}

// Decision is a subject's standing for one purpose. Found is false for a missing or unknown
// token; Status is empty when the subject never decided on the purpose.
type Decision struct {
	Subject uuid.UUID
	Status  domain.Status
	Found   bool
}

// Granted reports whether the purpose is granted.
func (d Decision) Granted() bool { return d.Status == domain.StatusGranted }

// Refused reports whether the subject rejected or withdrew the purpose.
func (d Decision) Refused() bool {
	return d.Status == domain.StatusRejected || d.Status == domain.StatusWithdrawn
}

// Decision returns the standing of the subject identified by token for purpose. Unlike
// Current it does not record the subject as seen, so the ingest hot path only reads.
func (s *Service) Decision(ctx context.Context, token string, purpose domain.Purpose) (Decision, error) {
	if token == "" {
		return Decision{}, nil
	}

	subject, err := s.repo.SubjectByToken(ctx, domain.HashToken(token))
	if errors.Is(err, domain.ErrNotFound) {
		return Decision{}, nil
	}

	if err != nil {
		return Decision{}, err
	}

	consents, err := s.repo.List(ctx, subject.UUID)
	if err != nil {
		return Decision{}, err
	}

	decision := Decision{Subject: subject.UUID, Found: true}

	for _, c := range consents {
		if c.Purpose == purpose {
			decision.Status = c.Status
		}
	}

	return decision, nil
}

// DeleteForUser removes the consent subjects linked to the user, for account erasure.
func (s *Service) DeleteForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.repo.DeleteForUser(ctx, userID)
}

func (s *Service) owns(ctx context.Context, subject domain.Subject, token string, userID *uuid.UUID) bool {
	if userID != nil && subject.UserUUID != nil && *userID == *subject.UserUUID {
		return true
	}

	if token == "" {
		return false
	}

	byToken, err := s.repo.SubjectByToken(ctx, domain.HashToken(token))

	return err == nil && byToken.UUID == subject.UUID
}

func validateDecisions(decisions map[domain.Purpose]bool, version string, userID *uuid.UUID) error {
	if len(decisions) == 0 {
		return fmt.Errorf("%w: purposes must include at least one decision", domain.ErrValidation)
	}

	for purpose := range decisions {
		if !domain.ValidPurpose(purpose) {
			return fmt.Errorf("%w: unknown purpose %q", domain.ErrValidation, purpose)
		}
	}

	if !domain.ValidPolicyVersion(version) {
		return fmt.Errorf("%w: policy_version must be 1-32 letters, digits, dots, underscores, or hyphens", domain.ErrValidation)
	}

	if decisions[domain.PurposeAuthenticatedAnalytics] {
		if userID == nil {
			return fmt.Errorf("%w: authenticated_analytics requires a signed-in user", domain.ErrValidation)
		}

		if granted, ok := decisions[domain.PurposeAnalytics]; !ok || !granted {
			return fmt.Errorf("%w: authenticated_analytics requires analytics to be granted", domain.ErrValidation)
		}
	}

	return nil
}

// consentPayload is AnalyticsConsentChanged in docs/architecture/asyncapi.yaml. It carries
// no token and no user id: consent subjects are pseudonymous.
type consentPayload struct {
	ConsentID     uuid.UUID `json:"consent_id"`
	SubjectID     uuid.UUID `json:"subject_id"`
	Purpose       string    `json:"purpose"`
	Status        string    `json:"status"`
	PolicyVersion string    `json:"policy_version"`
}

func consentEvent(c domain.Consent) event.Event {
	typ := event.AnalyticsConsentRejected

	switch c.Status {
	case domain.StatusGranted:
		typ = event.AnalyticsConsentGranted
	case domain.StatusWithdrawn:
		typ = event.AnalyticsConsentWithdrawn
	case domain.StatusRejected:
	}

	return event.New(typ, event.AggregateConsent, c.SubjectUUID, nil, c.DecidedAt, consentPayload{
		ConsentID: c.UUID, SubjectID: c.SubjectUUID, Purpose: string(c.Purpose),
		Status: string(c.Status), PolicyVersion: c.PolicyVersion,
	})
}

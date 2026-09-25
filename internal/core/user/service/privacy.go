package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/event"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/core/user/ports"
)

// WithPrivacy enables privacy updates: verifier checks the password proof that narrowing
// visibility requires, and events records user.privacy.updated with the change.
func (s *ProfileService) WithPrivacy(verifier ports.PasswordVerifier, events event.Unit) *ProfileService {
	s.verifier = verifier
	s.events = events

	return s
}

// Privacy returns the user's privacy settings.
func (s *ProfileService) Privacy(ctx context.Context, userID uuid.UUID) (userdomain.Privacy, error) {
	view, err := s.repo.GetView(ctx, userID)
	if err != nil {
		return userdomain.Privacy{}, err
	}

	return view.Privacy, nil
}

// UpdatePrivacy applies patch to the user's privacy settings. Narrowing profile visibility
// requires the current password; widening it or toggling the other flags does not.
func (s *ProfileService) UpdatePrivacy(
	ctx context.Context, userID uuid.UUID, patch userdomain.PrivacyPatch, password string,
) (userdomain.Privacy, error) {
	if patch.Empty() {
		return userdomain.Privacy{}, ErrEmptyPatch
	}

	current, err := s.Privacy(ctx, userID)
	if err != nil {
		return userdomain.Privacy{}, err
	}

	next, err := current.Apply(patch)
	if err != nil {
		return userdomain.Privacy{}, err
	}

	if next == current {
		return current, nil
	}

	proof := current.Visibility.Narrows(next.Visibility)
	if proof {
		if err := s.verifyPassword(ctx, userID, password); err != nil {
			return userdomain.Privacy{}, err
		}
	}

	now := s.clock.Now()

	err = s.events.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SavePrivacy(ctx, userID, next, now); err != nil {
			return err
		}

		return s.events.Record(ctx, event.New(event.UserPrivacyUpdated, event.AggregateUser, userID, &userID, now,
			privacyPayload{Diff: privacyDiff{Before: privacyFields(current), After: privacyFields(next)}, StepupProofPresent: proof}))
	})
	if err != nil {
		return userdomain.Privacy{}, err
	}

	for field, values := range changedPrivacy(current, next) {
		audit.AddChange(ctx, field, values[0], values[1])
	}

	audit.AddMetadata(ctx, "stepup_proof_present", proof)
	s.invalidate(ctx)

	return next, nil
}

func (s *ProfileService) verifyPassword(ctx context.Context, userID uuid.UUID, password string) error {
	if s.verifier == nil || password == "" {
		return userdomain.ErrPrivacyReauth
	}

	ok, err := s.verifier.VerifyPassword(ctx, userID, password)
	if err != nil {
		return err
	}

	if !ok {
		return userdomain.ErrPrivacyReauth
	}

	return nil
}

type privacyPayload struct {
	Diff               privacyDiff `json:"diff"`
	StepupProofPresent bool        `json:"stepup_proof_present"`
}

type privacyDiff struct {
	Before map[string]any `json:"before"`
	After  map[string]any `json:"after"`
}

func privacyFields(p userdomain.Privacy) map[string]any {
	return map[string]any{
		"visibility_profile":    string(p.Visibility),
		"visibility_email":      p.ShowEmail,
		"visibility_contact":    p.ShowContact,
		"search_allow_indexing": p.AllowIndexing,
	}
}

// changedPrivacy maps each changed field to its before and after values.
func changedPrivacy(before, after userdomain.Privacy) map[string][2]any {
	b, a := privacyFields(before), privacyFields(after)
	changed := make(map[string][2]any, len(a))

	for field, value := range a {
		if b[field] != value {
			changed[field] = [2]any{b[field], value}
		}
	}

	return changed
}

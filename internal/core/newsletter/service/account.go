package service

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/newsletter/domain"
	"github.com/turahe/blog-api/internal/core/newsletter/ports"
)

// MySubscription returns the signed-in user's subscription, matched by account link or by the
// account email, and the lists they can choose from. found is false when there is none.
func (s *Service) MySubscription(ctx context.Context, userID uuid.UUID) (Preferences, bool, error) {
	lists, err := s.Repo.Lists(ctx, false)
	if err != nil {
		return Preferences{}, false, err
	}

	sub, found, err := s.forAccount(ctx, userID)
	if err != nil || !found {
		return Preferences{Lists: lists}, false, err
	}

	return Preferences{Subscriber: sub, Lists: lists}, true, nil
}

// MySubscribeInput subscribes the signed-in user's account email.
type MySubscribeInput struct {
	UserID    uuid.UUID
	Lists     []string
	Format    string
	IP        string
	UserAgent string
}

// MySubscribe subscribes the account email to lists (the default lists when empty). A verified
// account email skips the confirmation email unless double_optin_required is set; otherwise
// the lists stay pending until the emailed link is used.
func (s *Service) MySubscribe(ctx context.Context, in MySubscribeInput) (Preferences, error) {
	if s.Accounts == nil {
		return Preferences{}, domain.ErrNotConfigured
	}

	account, err := s.Accounts.Account(ctx, in.UserID)
	if err != nil {
		return Preferences{}, err
	}

	email, err := domain.NormalizeEmail(account.Email)
	if err != nil {
		return Preferences{}, err
	}

	lists, err := s.resolveLists(ctx, in.Lists)
	if err != nil {
		return Preferences{}, err
	}

	cfg, err := s.Repo.Config(ctx)
	if err != nil {
		return Preferences{}, err
	}

	var (
		confirm *ports.ConfirmEmail
		subID   uuid.UUID
	)

	err = s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, found, err := s.forAccount(ctx, in.UserID)
		if err != nil {
			return err
		}

		if !found {
			name, _ := cleanName(account.Name)
			sub = s.newSubscriber(email, name, domain.FormatHTML, domain.SourceAccount, hashIdentity(in.IP), in.UserAgent)
		}

		sub.UserID, subID = &in.UserID, sub.UUID

		if in.Format != "" {
			if sub.Format, err = domain.ParseFormat(in.Format); err != nil {
				return err
			}
		}

		if account.EmailVerified && !cfg.DoubleOptInRequired {
			return s.joinNow(ctx, &sub, lists, !found, hashIdentity(in.IP))
		}

		confirm, err = s.requestConfirmation(ctx, &sub, lists, cfg, !found, hashIdentity(in.IP), string(domain.SourceAccount))

		return err
	})
	if err != nil {
		return Preferences{}, err
	}

	s.sendConfirm(ctx, confirm)

	return s.preferences(ctx, subID)
}

// joinNow activates lists for a verified account without a confirmation email.
func (s *Service) joinNow(ctx context.Context, sub *domain.Subscriber, lists []domain.List, create bool, ipHash string) error {
	now := s.Clock.Now()

	var joined []string

	for _, l := range lists {
		if m, ok := sub.Membership(l.Slug); !ok || m.State != domain.MembershipActive {
			sub.SetMembership(l.Slug, domain.MembershipActive, now)
			joined = append(joined, l.Slug)
		}
	}

	source := string(domain.SourceAccount)
	events := s.listEvents(*sub, domain.ConsentListJoined, joined, source, now)
	change := "lists_changed"

	if sub.Status != domain.StatusActive {
		kind := domain.ConsentResubscribed
		if create || sub.Status == domain.StatusPending {
			kind = domain.ConsentSubscribed
		}

		activate(sub, now)
		events = append(events, s.consent(*sub, kind, "", source, ipHash, now))
		change = "confirmed"
	}

	if create {
		sub.UpdatedAt = now
		if err := s.Repo.CreateSubscriber(ctx, *sub); err != nil {
			return err
		}
	} else if err := s.Repo.UpdateSubscriber(ctx, *sub); err != nil {
		return err
	}

	if len(events) == 0 {
		return nil
	}

	if err := s.Repo.AppendConsent(ctx, events...); err != nil {
		return err
	}

	return s.Events.Record(ctx, subscriberEvent(*sub, change, nil, now))
}

// MyUnsubscribe leaves lists, or every list when lists is empty.
func (s *Service) MyUnsubscribe(ctx context.Context, userID uuid.UUID, lists []string, reason, feedback, ip string) (Preferences, error) {
	if err := domain.ValidateFeedback(reason, feedback); err != nil {
		return Preferences{}, err
	}

	var subID uuid.UUID

	err := s.Events.InTx(ctx, func(ctx context.Context) error {
		sub, found, err := s.forAccount(ctx, userID)
		if err != nil {
			return err
		}

		if !found {
			return domain.ErrNotFound
		}

		subID = sub.UUID
		source := string(domain.SourceAccount)

		if len(lists) == 0 {
			return s.unsubscribeAll(ctx, &sub, source, reason, feedback, hashIdentity(ip), nil)
		}

		leaving, err := s.resolveExact(ctx, lists)
		if err != nil {
			return err
		}

		keep := make([]string, 0, len(sub.Memberships))

		for _, slug := range sub.ActiveLists() {
			if !slices.Contains(leaving, slug) {
				keep = append(keep, slug)
			}
		}

		return s.applyChoices(ctx, &sub, nil, &keep, source, hashIdentity(ip), nil)
	})
	if err != nil {
		return Preferences{}, err
	}

	return s.preferences(ctx, subID)
}

// forAccount finds the user's subscriber by account link, then by the account's email.
func (s *Service) forAccount(ctx context.Context, userID uuid.UUID) (domain.Subscriber, bool, error) {
	sub, err := s.Repo.SubscriberByUser(ctx, userID)
	if err == nil {
		return sub, true, nil
	}

	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Subscriber{}, false, err
	}

	if s.Accounts == nil {
		return domain.Subscriber{}, false, nil
	}

	account, err := s.Accounts.Account(ctx, userID)
	if err != nil {
		return domain.Subscriber{}, false, err
	}

	email, err := domain.NormalizeEmail(account.Email)
	if err != nil {
		return domain.Subscriber{}, false, nil //nolint:nilerr // an account without a usable email has no subscription
	}

	return s.byEmail(ctx, email)
}

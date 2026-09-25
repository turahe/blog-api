package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/turahe/blog-api/internal/core/audit"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	"github.com/turahe/blog-api/internal/core/auth/ports"
	"github.com/turahe/blog-api/internal/core/readcache"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// maxLiveRegistrations caps unexpired sign-ups per address, so the endpoint cannot be used
// to flood someone's inbox with verification emails.
const maxLiveRegistrations = 3

type signupDeps struct {
	registrations ports.RegistrationRepository
	policy        ports.RegistrationPolicy
	notifier      ports.RegistrationNotifier
}

// WithRegistration enables public sign-up. Without it, or while policy reports sign-up
// closed, Register and VerifyEmail return authdomain.ErrRegistrationClosed.
func (s *AuthService) WithRegistration(
	registrations ports.RegistrationRepository, policy ports.RegistrationPolicy, notifier ports.RegistrationNotifier,
) *AuthService {
	s.signup = signupDeps{registrations: registrations, policy: policy, notifier: notifier}

	return s
}

// Register starts a sign-up and emails a verification token. It answers the same whether or
// not the address already has an account: an existing account is told about the attempt
// instead, so the endpoint cannot be used to discover registered addresses. Usernames are
// public, so a taken one is reported.
func (s *AuthService) Register(ctx context.Context, in authdomain.SignUp) error {
	if err := s.registrationOpen(ctx); err != nil {
		return err
	}

	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Username = strings.TrimSpace(in.Username)
	in.FullName = strings.TrimSpace(in.FullName)

	err := validateNewUser(authdomain.NewUser{Email: in.Email, Username: in.Username, FullName: in.FullName, Password: in.Password})
	if err != nil {
		return err
	}

	if err := s.usernameFree(ctx, in.Username); err != nil {
		return err
	}

	// Hashed on every path so an existing address does not answer faster.
	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return err
	}

	existing, err := s.users.FindByEmail(ctx, in.Email)
	if err == nil {
		s.notifyRegistration(ctx, func(ctx context.Context, n ports.RegistrationNotifier) { n.AccountExists(ctx, existing) })
		return nil
	}

	if !errors.Is(err, userdomain.ErrNotFound) {
		return err
	}

	return s.startRegistration(ctx, in, hash)
}

func (s *AuthService) startRegistration(ctx context.Context, in authdomain.SignUp, passwordHash string) error {
	raw, tokenHash, _, err := s.tokens.IssueResetToken()
	if err != nil {
		return err
	}

	now := s.clock.Now()
	registration := authdomain.Registration{
		UUID: s.ids.New(), Email: in.Email, Username: in.Username, FullName: in.FullName,
		PasswordHash: passwordHash, TokenHash: tokenHash, ExpiresAt: now.Add(s.cfg.RegistrationTTL), CreatedAt: now,
	}

	stored, err := s.signup.registrations.Create(ctx, registration, maxLiveRegistrations)
	if err != nil || !stored {
		return err
	}

	if s.sink != nil {
		s.sink.Capture(raw)
	}

	s.notifyRegistration(ctx, func(ctx context.Context, n ports.RegistrationNotifier) { n.AccountVerify(ctx, registration, raw) })

	return nil
}

// VerifyEmail turns the registration behind rawToken into an active, verified account and
// signs it in. The sign-up password is required as well as the token, so neither the person
// who registered someone else's address nor the owner of an address someone else registered
// can activate the account alone. A wrong password counts toward the login lockout.
func (s *AuthService) VerifyEmail(ctx context.Context, rawToken, password, userAgent, ip string) (authdomain.TokenPair, error) {
	if err := s.registrationOpen(ctx); err != nil {
		return authdomain.TokenPair{}, err
	}

	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || password == "" {
		return authdomain.TokenPair{}, fmt.Errorf("%w: token and password required", authdomain.ErrValidation)
	}

	registration, err := s.signup.registrations.FindByTokenHash(ctx, s.tokens.HashResetToken(rawToken))
	if err != nil {
		return authdomain.TokenPair{}, err
	}

	now := s.clock.Now()
	if !registration.ExpiresAt.After(now) {
		return authdomain.TokenPair{}, authdomain.ErrRegistrationTokenExpired
	}

	if err := s.checkLocked(ctx, registration.Email); err != nil {
		return authdomain.TokenPair{}, err
	}

	if !s.hasher.Compare(registration.PasswordHash, password) {
		return authdomain.TokenPair{}, s.loginFailed(ctx, registration.Email)
	}

	user, err := s.createRegisteredUser(ctx, registration)
	if err != nil {
		return authdomain.TokenPair{}, err
	}

	readcache.Invalidate(ctx, s.cache, readcache.Users)

	return s.completeLogin(ctx, user, userAgent, ip, false)
}

func (s *AuthService) createRegisteredUser(ctx context.Context, registration authdomain.Registration) (userdomain.User, error) {
	now := s.clock.Now()

	var user userdomain.User

	err := s.events.InTx(ctx, func(ctx context.Context) error {
		consumed, err := s.signup.registrations.Consume(ctx, registration.TokenHash)
		if err != nil {
			return err
		}

		if !consumed {
			return authdomain.ErrRegistrationTokenInvalid
		}

		user, err = s.users.Create(ctx, userdomain.User{
			UUID: s.ids.New(), Email: registration.Email, Username: registration.Username, FullName: registration.FullName,
			PasswordHash: registration.PasswordHash, Status: userdomain.StatusActive, EmailVerifiedAt: &now,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}

		audit.SetActor(ctx, user.UUID)

		return s.events.Record(ctx, userCreatedEvent(user, nil))
	})

	return user, err
}

func (s *AuthService) registrationOpen(ctx context.Context) error {
	if s.signup.registrations == nil || s.signup.policy == nil {
		return authdomain.ErrRegistrationClosed
	}

	open, err := s.signup.policy.RegistrationOpen(ctx)
	if err != nil {
		return fmt.Errorf("check registration policy: %w", err)
	}

	if !open {
		return authdomain.ErrRegistrationClosed
	}

	return nil
}

// notifyRegistration delivers in the background, so a slow mail server cannot reveal which
// branch Register took.
func (s *AuthService) notifyRegistration(ctx context.Context, send func(context.Context, ports.RegistrationNotifier)) {
	if s.signup.notifier == nil {
		return
	}

	deliverLater(ctx, func(ctx context.Context) { send(ctx, s.signup.notifier) })
}

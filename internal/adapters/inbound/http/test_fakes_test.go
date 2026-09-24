package http

import (
	"context"
	"time"

	"github.com/google/uuid"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
)

// test doubles shared across http package tests.
type fakeAuthService struct {
	parseAccessFn func(token string) (authdomain.AccessClaims, error)
}

func (f fakeAuthService) Login(context.Context, string, string, string, string, bool) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}

func (f fakeAuthService) Refresh(context.Context, string, string, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}

func (f fakeAuthService) Logout(context.Context, uuid.UUID, string) error { return nil }

func (f fakeAuthService) ParseAccessToken(token string) (authdomain.AccessClaims, error) {
	if f.parseAccessFn != nil {
		return f.parseAccessFn(token)
	}

	return authdomain.AccessClaims{}, nil
}

func (f fakeAuthService) ForgotPassword(context.Context, string) error { return nil }

func (f fakeAuthService) CheckResetToken(context.Context, string) (authdomain.ResetTokenValidity, error) {
	return authdomain.ResetTokenValidity{}, nil
}

func (f fakeAuthService) ResetPassword(context.Context, string, string, string) error { return nil }

func (f fakeAuthService) ChangePassword(context.Context, uuid.UUID, string, string, string, bool) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

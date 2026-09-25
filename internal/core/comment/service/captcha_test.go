package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
)

type fakeCaptcha struct {
	ok    bool
	err   error
	calls int
	token string
	ip    string
}

func (f *fakeCaptcha) Verify(_ context.Context, token, remoteIP string) (bool, error) {
	f.calls++
	f.token, f.ip = token, remoteIP

	return f.ok, f.err
}

func guestInput(post [16]byte, token string) CreateInput {
	return CreateInput{
		PostUUID: post, AuthorName: "Ann", AuthorEmail: "ann@example.com", Content: "hi",
		CaptchaToken: token, ClientIP: "203.0.113.9",
	}
}

func TestCaptchaGuardsGuestCreate(t *testing.T) {
	t.Parallel()

	captcha := &fakeCaptcha{ok: true}
	f := newFixture(Config{GuestEnabled: true, Captcha: captcha})

	_, err := f.svc.Create(context.Background(), guestInput(f.post, "tok"))
	require.NoError(t, err)
	require.Equal(t, 1, captcha.calls)
	require.Equal(t, "tok", captcha.token)
	require.Equal(t, "203.0.113.9", captcha.ip)

	captcha.ok = false
	_, err = f.svc.Create(context.Background(), guestInput(f.post, "bad"))
	require.ErrorIs(t, err, commentdomain.ErrChallengeFailed)

	captcha.err = errors.New("timeout")
	_, err = f.svc.Create(context.Background(), guestInput(f.post, "tok"))
	require.ErrorIs(t, err, commentdomain.ErrChallengeUnavailable)
}

func TestCaptchaSkipsSignedInUsersAndHoneypotHits(t *testing.T) {
	t.Parallel()

	captcha := &fakeCaptcha{}
	f := newFixture(Config{GuestEnabled: true, Captcha: captcha})

	_, err := f.svc.Create(context.Background(), CreateInput{PostUUID: f.post, AuthorUUID: &f.user, Content: "hi"})
	require.NoError(t, err)

	in := guestInput(f.post, "")
	in.Honeypot = "bot"
	got, err := f.svc.Create(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, commentdomain.StatusSpam, got.Status)

	require.Zero(t, captcha.calls)
}

func TestCaptchaOffWithoutVerifier(t *testing.T) {
	t.Parallel()

	f := newFixture(Config{GuestEnabled: true})

	_, err := f.svc.Create(context.Background(), guestInput(f.post, ""))
	require.NoError(t, err)
}

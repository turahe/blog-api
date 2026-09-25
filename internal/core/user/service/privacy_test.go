package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakeVerifier struct {
	password string
	calls    int
}

func (v *fakeVerifier) VerifyPassword(_ context.Context, _ uuid.UUID, password string) (bool, error) {
	v.calls++
	return password == v.password, nil
}

func TestVisibilityNarrows(t *testing.T) {
	t.Parallel()

	public, unlisted, private := userdomain.VisibilityPublic, userdomain.VisibilityUnlisted, userdomain.VisibilityPrivate
	require.True(t, public.Narrows(private))
	require.True(t, public.Narrows(unlisted))
	require.True(t, unlisted.Narrows(private))
	require.False(t, private.Narrows(public))
	require.False(t, unlisted.Narrows(unlisted))
	require.False(t, userdomain.Visibility("friends").Valid())
}

func TestUpdatePrivacy(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*ProfileService, *fakeProfileRepo, *fakeVerifier, *eventtest.Recorder, *readcachetest.Memory, uuid.UUID) {
		t.Helper()

		view := activeView("ada")
		repo := newFakeProfileRepo(view)
		verifier := &fakeVerifier{password: "correct horse"}
		events := &eventtest.Recorder{}
		cache := readcachetest.New()
		svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithCache(cache).WithPrivacy(verifier, events.Unit())

		return svc, repo, verifier, events, cache, view.User.UUID
	}

	t.Run("widening and flags need no password", func(t *testing.T) {
		t.Parallel()

		svc, repo, verifier, events, cache, id := setup(t)

		got, err := svc.UpdatePrivacy(t.Context(), id, userdomain.PrivacyPatch{ShowEmail: new(true)}, "")
		require.NoError(t, err)
		require.True(t, got.ShowEmail)
		require.True(t, repo.views[id].Privacy.ShowEmail)
		require.Zero(t, verifier.calls)
		require.Equal(t, []string{event.UserPrivacyUpdated}, events.Types())
		require.Equal(t, 1, cache.Invalidations(readcache.Users))
	})

	t.Run("narrowing requires the current password", func(t *testing.T) {
		t.Parallel()

		svc, repo, _, events, _, id := setup(t)
		patch := userdomain.PrivacyPatch{Visibility: new(userdomain.VisibilityPrivate)}

		_, err := svc.UpdatePrivacy(t.Context(), id, patch, "")
		require.ErrorIs(t, err, userdomain.ErrPrivacyReauth)

		_, err = svc.UpdatePrivacy(t.Context(), id, patch, "wrong")
		require.ErrorIs(t, err, userdomain.ErrPrivacyReauth)
		require.Zero(t, repo.saved)
		require.Empty(t, events.Types())

		got, err := svc.UpdatePrivacy(t.Context(), id, patch, "correct horse")
		require.NoError(t, err)
		require.Equal(t, userdomain.VisibilityPrivate, got.Visibility)

		payload, ok := events.Events()[0].Payload.(privacyPayload)
		require.True(t, ok)
		require.True(t, payload.StepupProofPresent)
		require.Equal(t, "public", payload.Diff.Before["visibility_profile"])
		require.Equal(t, "private", payload.Diff.After["visibility_profile"])
	})

	t.Run("no-op and invalid patches", func(t *testing.T) {
		t.Parallel()

		svc, repo, _, events, _, id := setup(t)

		_, err := svc.UpdatePrivacy(t.Context(), id, userdomain.PrivacyPatch{}, "")
		require.ErrorIs(t, err, ErrEmptyPatch)

		_, err = svc.UpdatePrivacy(t.Context(), id, userdomain.PrivacyPatch{Visibility: new(userdomain.Visibility("friends"))}, "")
		require.ErrorIs(t, err, userdomain.ErrProfileValidation)

		got, err := svc.UpdatePrivacy(t.Context(), id, userdomain.PrivacyPatch{ShowContact: new(true)}, "")
		require.NoError(t, err)
		require.True(t, got.ShowContact)
		require.Zero(t, repo.saved, "unchanged settings are not rewritten")
		require.Empty(t, events.Types())

		_, err = svc.UpdatePrivacy(t.Context(), uuid.New(), userdomain.PrivacyPatch{ShowEmail: new(true)}, "")
		require.ErrorIs(t, err, userdomain.ErrNotFound)
	})
}

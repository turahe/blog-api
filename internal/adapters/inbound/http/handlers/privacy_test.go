package handlers

import (
	"context"
	nethttp "net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fakePrivacy struct {
	settings userdomain.Privacy
	patch    userdomain.PrivacyPatch
	password string
	err      error
}

func (f *fakePrivacy) Privacy(context.Context, uuid.UUID) (userdomain.Privacy, error) {
	return f.settings, f.err
}

func (f *fakePrivacy) UpdatePrivacy(_ context.Context, _ uuid.UUID, patch userdomain.PrivacyPatch, password string) (userdomain.Privacy, error) {
	f.patch, f.password = patch, password
	if f.err != nil {
		return userdomain.Privacy{}, f.err
	}

	next, err := f.settings.Apply(patch)
	f.settings = next

	return next, err
}

func TestPrivacyHandlers(t *testing.T) {
	t.Parallel()

	user := testUserID
	json := "application/json"

	t.Run("get", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacy{settings: userdomain.DefaultPrivacy()}
		w, body := runProfile(t, meGetPrivacyHandler(fake), profileRequest{method: nethttp.MethodGet, target: "/me/privacy", user: &user})
		require.Equal(t, nethttp.StatusOK, w.Code)
		require.Equal(t, "public", dataOf(body)["visibilityProfile"])
		require.Equal(t, true, dataOf(body)["searchAllowIndexing"])
	})

	t.Run("put passes the patch and password", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacy{settings: userdomain.DefaultPrivacy()}
		w, body := runProfile(t, meUpdatePrivacyHandler(fake), profileRequest{
			method: nethttp.MethodPut, target: "/me/privacy", contentType: json, user: &user,
			body: `{"visibilityProfile":"private","visibilityEmail":true,"currentPassword":"pw"}`,
		})
		require.Equal(t, nethttp.StatusOK, w.Code)
		require.Equal(t, userdomain.VisibilityPrivate, *fake.patch.Visibility)
		require.True(t, *fake.patch.ShowEmail)
		require.Nil(t, fake.patch.ShowContact)
		require.Equal(t, "pw", fake.password)
		require.Equal(t, "private", dataOf(body)["visibilityProfile"])
	})

	t.Run("put rejects unknown visibility", func(t *testing.T) {
		t.Parallel()

		w, _ := runProfile(t, meUpdatePrivacyHandler(&fakePrivacy{}), profileRequest{
			method: nethttp.MethodPut, target: "/me/privacy", contentType: json, user: &user,
			body: `{"visibilityProfile":"friends"}`,
		})
		require.Equal(t, nethttp.StatusBadRequest, w.Code)
	})

	t.Run("missing proof is 403", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacy{err: userdomain.ErrPrivacyReauth}
		w, body := runProfile(t, meUpdatePrivacyHandler(fake), profileRequest{
			method: nethttp.MethodPut, target: "/me/privacy", contentType: json, user: &user,
			body: `{"visibilityProfile":"private"}`,
		})
		require.Equal(t, nethttp.StatusForbidden, w.Code)
		require.Equal(t, "privacy.level_change_requires_reauth", errorCode(body))
	})
}

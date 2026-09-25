package handlers

import (
	"context"
	nethttp "net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	privacydomain "github.com/turahe/blog-api/internal/core/privacy/domain"
	privacyservice "github.com/turahe/blog-api/internal/core/privacy/service"
)

type fakePrivacyRequests struct {
	export   privacyservice.Export
	erase    privacydomain.Request
	password string
	err      error
}

func (f *fakePrivacyRequests) RequestExport(context.Context, uuid.UUID) (privacyservice.Export, bool, error) {
	return f.export, false, f.err
}

func (f *fakePrivacyRequests) RequestErase(_ context.Context, _ uuid.UUID, password string) (privacydomain.Request, bool, error) {
	f.password = password
	return f.erase, true, f.err
}

func TestExportHandler(t *testing.T) {
	t.Parallel()

	user := testUserID
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	pending := privacydomain.Request{UUID: uuid.New(), Kind: privacydomain.KindExport, Status: privacydomain.StatusPending, CreatedAt: now}

	t.Run("pending is 202", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacyRequests{export: privacyservice.Export{Request: pending}}
		w, body := runProfile(t, meExportHandler(fake), profileRequest{method: nethttp.MethodGet, target: "/me/activity/export", user: &user})
		require.Equal(t, nethttp.StatusAccepted, w.Code)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.Equal(t, "pending", dataOf(body)["status"])
		require.NotContains(t, dataOf(body), "download_url")
	})

	t.Run("ready is 200 with a link", func(t *testing.T) {
		t.Parallel()

		key, archiveExpiry, linkExpiry := "privacy-exports/k.json", now.Add(72*time.Hour), now.Add(15*time.Minute)
		ready := pending
		ready.Status, ready.StorageKey, ready.ExpiresAt, ready.CompletedAt = privacydomain.StatusCompleted, &key, &archiveExpiry, &now
		fake := &fakePrivacyRequests{export: privacyservice.Export{
			Request: ready, DownloadURL: "https://s3.test/k.json?sig", DownloadExpiresAt: &linkExpiry,
		}}

		w, body := runProfile(t, meExportHandler(fake), profileRequest{method: nethttp.MethodGet, target: "/me/activity/export", user: &user})
		require.Equal(t, nethttp.StatusOK, w.Code)
		require.Equal(t, "https://s3.test/k.json?sig", dataOf(body)["download_url"])
		require.Equal(t, linkExpiry.Format(time.RFC3339), dataOf(body)["download_expires_at"])
		require.NotContains(t, dataOf(body), "storage_key")
	})

	t.Run("no storage is 503", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacyRequests{err: privacydomain.ErrExportUnavailable}
		w, body := runProfile(t, meExportHandler(fake), profileRequest{method: nethttp.MethodGet, target: "/me/activity/export", user: &user})
		require.Equal(t, nethttp.StatusServiceUnavailable, w.Code)
		require.Equal(t, "privacy.export_unavailable", errorCode(body))
	})
}

func TestEraseHandler(t *testing.T) {
	t.Parallel()

	user := testUserID
	request := privacydomain.Request{UUID: uuid.New(), Kind: privacydomain.KindErase, Status: privacydomain.StatusPending, CreatedAt: time.Now()}

	t.Run("queued is 202", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacyRequests{erase: request}
		w, body := runProfile(t, meEraseHandler(fake), profileRequest{
			method: nethttp.MethodPost, target: "/me/activity/erase", contentType: "application/json", user: &user,
			body: `{"current_password":"pw"}`,
		})
		require.Equal(t, nethttp.StatusAccepted, w.Code)
		require.Equal(t, "pw", fake.password)
		require.Equal(t, "erase", dataOf(body)["kind"])
	})

	t.Run("password is required", func(t *testing.T) {
		t.Parallel()

		w, _ := runProfile(t, meEraseHandler(&fakePrivacyRequests{}), profileRequest{
			method: nethttp.MethodPost, target: "/me/activity/erase", contentType: "application/json", user: &user, body: `{}`,
		})
		require.Equal(t, nethttp.StatusBadRequest, w.Code)
	})

	t.Run("wrong password is 403", func(t *testing.T) {
		t.Parallel()

		fake := &fakePrivacyRequests{err: privacydomain.ErrReauth}
		w, body := runProfile(t, meEraseHandler(fake), profileRequest{
			method: nethttp.MethodPost, target: "/me/activity/erase", contentType: "application/json", user: &user,
			body: `{"current_password":"nope"}`,
		})
		require.Equal(t, nethttp.StatusForbidden, w.Code)
		require.Equal(t, "privacy.erase_requires_reauth", errorCode(body))
	})
}

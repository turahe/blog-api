package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/event"
	"github.com/turahe/blog-api/internal/core/event/eventtest"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

func TestMediaWritesRecordEvents(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService()
	events := &eventtest.Recorder{}
	svc.WithEvents(events.Unit())

	owner := uuid.New()
	asset, err := svc.UploadImage(t.Context(), mediadomain.ImageUpload{
		UploadedBy: &owner, Filename: "a.png", Data: encodePNG(t, 2, 2),
	})
	require.NoError(t, err)
	require.NoError(t, svc.Delete(t.Context(), asset.UUID))

	require.Equal(t, []string{event.MediaUploaded, event.MediaDeleted}, events.Types())

	uploaded := events.Events()[0]
	require.Equal(t, &owner, uploaded.ActorID)
	require.Equal(t, mediaPayload{
		MediaAssetID: asset.UUID, StorageKey: asset.StorageKey, Disk: asset.Disk,
		ContentType: "image/png", SizeBytes: asset.SizeBytes,
	}, uploaded.Payload)
}

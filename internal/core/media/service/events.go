package service

import (
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/event"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

// mediaPayload is MediaEventPayload in docs/architecture/asyncapi.yaml.
type mediaPayload struct {
	MediaAssetID uuid.UUID `json:"media_asset_id"`
	StorageKey   string    `json:"storage_key"`
	Disk         string    `json:"disk"`
	ContentType  string    `json:"content_type,omitempty"`
	SizeBytes    int64     `json:"size_bytes,omitempty"`
}

func mediaEvent(typ string, asset mediadomain.MediaAsset, at time.Time) event.Event {
	return event.New(typ, event.AggregateMedia, asset.UUID, asset.UploadedByUUID, at, mediaPayload{
		MediaAssetID: asset.UUID, StorageKey: asset.StorageKey, Disk: asset.Disk,
		ContentType: asset.ContentType, SizeBytes: asset.SizeBytes,
	})
}

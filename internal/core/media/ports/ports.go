package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

// ObjectInfo is storage metadata of an uploaded object.
type ObjectInfo struct {
	Size        int64
	ContentType string
	ETag        string
}

// ObjectStorage presigns uploads and inspects stored objects.
type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, headers map[string]string, err error)
	HeadObject(ctx context.Context, key string) (ObjectInfo, error)
	PutObject(ctx context.Context, key, contentType string, body []byte) error
	// ReadPrefix returns at most n leading bytes of the object.
	ReadPrefix(ctx context.Context, key string, n int64) ([]byte, error)
	// DeleteObject removes the object; a missing object is not an error.
	DeleteObject(ctx context.Context, key string) error
}

// Repository stores media assets.
type Repository interface {
	Create(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error)
	GetByID(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	Update(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error)
	List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error)
	SoftDelete(ctx context.Context, id uuid.UUID, deletedAt time.Time) error
	ClearEntityReferences(ctx context.Context, id uuid.UUID) error

	// Abandoned returns live assets that never became ready and whose presign expired before
	// the given time, oldest first.
	Abandoned(ctx context.Context, expiredBefore time.Time, limit int) ([]mediadomain.MediaAsset, error)
	// Trashed returns assets soft-deleted before the given time, oldest first.
	Trashed(ctx context.Context, deletedBefore time.Time, limit int) ([]mediadomain.MediaAsset, error)
	// Purge removes the row, but only while it is still abandoned or trashed; it reports
	// whether a row was removed.
	Purge(ctx context.Context, id uuid.UUID) (bool, error)
	Usage(ctx context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error)
}

// PolicySource reads the admin-set transform presets and defaults.
type PolicySource interface {
	TransformPolicy(ctx context.Context) (mediadomain.TransformPolicy, error)
}

// PostMediaRepository stores post media attachments.
type PostMediaRepository interface {
	ReplaceAll(ctx context.Context, postID uuid.UUID, items []mediadomain.PostMediaItem) error
	ListByPostID(ctx context.Context, postID uuid.UUID) ([]mediadomain.PostMediaItem, error)
}

// Transformer returns a URL that serves asset resized per t. The URL must be signed so its
// parameters cannot be changed, and should expire so deleted assets stop being served.
type Transformer interface {
	URL(asset mediadomain.MediaAsset, t mediadomain.Transform) (string, error)
}

// Service is the media use-case API consumed by HTTP handlers.
type Service interface {
	PresignUpload(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error)
	CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error)
	Get(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	GetReady(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateTags(ctx context.Context, id uuid.UUID, tags []string) (mediadomain.MediaAsset, error)
	UploadImage(ctx context.Context, input mediadomain.ImageUpload) (mediadomain.MediaAsset, error)
	TransformURL(ctx context.Context, id uuid.UUID, t mediadomain.Transform) (string, error)
	// Variants returns the signed preset URLs of each ready raster asset, keyed by asset and
	// preset name. It returns nil when transforms are not configured.
	Variants(ctx context.Context, assets ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error)
	Usage(ctx context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error)
}

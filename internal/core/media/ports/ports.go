package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

type ObjectInfo struct {
	Size        int64
	ContentType string
	ETag        string
}

type ObjectStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, headers map[string]string, err error)
	HeadObject(ctx context.Context, key string) (ObjectInfo, error)
}

type Repository interface {
	Create(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error)
	GetByID(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	Update(ctx context.Context, asset mediadomain.MediaAsset) (mediadomain.MediaAsset, error)
}

type Service interface {
	PresignUpload(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error)
	CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
}

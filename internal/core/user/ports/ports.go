// Package ports declares the user repository and service interfaces.
package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Repository reads user accounts.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error)
}

// Service is the user use-case API consumed by HTTP handlers.
type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (userdomain.User, error)
	List(ctx context.Context, page, perPage int) ([]userdomain.User, int64, error)
}

// ProfileRepository stores profiles, privacy settings, and avatar references.
type ProfileRepository interface {
	// GetView returns the live user matching the UUID; missing profile rows read as defaults.
	GetView(ctx context.Context, userID uuid.UUID) (userdomain.ProfileView, error)
	// GetViewByUsername is GetView keyed by case-insensitive username.
	GetViewByUsername(ctx context.Context, username string) (userdomain.ProfileView, error)
	// SaveProfile upserts the profile and, when fullName is non-nil, the user's full name.
	// A duplicate display name is reported as userdomain.ErrDisplayNameTaken.
	SaveProfile(ctx context.Context, userID uuid.UUID, fullName *string, profile userdomain.Profile, updatedBy uuid.UUID, at time.Time) error
	// SetAvatar points the user at a media asset, or clears it when mediaID is nil.
	SetAvatar(ctx context.Context, userID uuid.UUID, mediaID *uuid.UUID, at time.Time) error
}

// AvatarStore uploads and removes avatar images.
type AvatarStore interface {
	UploadImage(ctx context.Context, input mediadomain.ImageUpload) (mediadomain.MediaAsset, error)
	Get(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

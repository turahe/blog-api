package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/core/audit"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/readcache"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	"github.com/turahe/blog-api/internal/core/user/ports"
)

// Profile service errors.
var (
	ErrEmptyPatch         = errors.New("profile patch has no fields")
	ErrAvatarsUnavailable = errors.New("avatar storage is not configured")
)

// AvatarTag marks media assets uploaded as avatars so replacing one only removes avatar uploads.
const AvatarTag = "avatar"

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// ProfileService manages user profiles, avatars, and the public profile view.
type ProfileService struct {
	repo           ports.ProfileRepository
	clock          Clock
	avatars        ports.AvatarStore
	avatarMaxBytes int64
	cache          readcache.Cache
}

// NewProfileService returns a ProfileService; avatars stay unavailable until WithAvatars.
func NewProfileService(repo ports.ProfileRepository, clock Clock) *ProfileService {
	return &ProfileService{repo: repo, clock: clock}
}

// WithAvatars enables avatar upload and removal through store, capping uploads at maxBytes.
func (s *ProfileService) WithAvatars(store ports.AvatarStore, maxBytes int64) *ProfileService {
	s.avatars = store
	s.avatarMaxBytes = maxBytes

	return s
}

// WithCache caches public profile reads; profile writes invalidate them.
func (s *ProfileService) WithCache(cache readcache.Cache) *ProfileService {
	s.cache = cache
	return s
}

// Get returns the full profile view of a live user.
func (s *ProfileService) Get(ctx context.Context, userID uuid.UUID) (userdomain.ProfileView, error) {
	return s.repo.GetView(ctx, userID)
}

// Update applies an allowlisted patch to target's profile on behalf of actor.
func (s *ProfileService) Update(ctx context.Context, actor, target uuid.UUID, patch userdomain.ProfilePatch) (userdomain.ProfileView, error) {
	if isEmptyPatch(patch) {
		return userdomain.ProfileView{}, ErrEmptyPatch
	}

	view, err := s.repo.GetView(ctx, target)
	if err != nil {
		return userdomain.ProfileView{}, err
	}

	now := s.clock.Now()
	profile := view.Profile

	fullName, err := applyPatch(&profile, patch, now)
	if err != nil {
		return userdomain.ProfileView{}, err
	}

	if err := s.repo.SaveProfile(ctx, target, fullName, profile, actor, now); err != nil {
		return userdomain.ProfileView{}, err
	}

	// Field names only: profile values are personal data kept out of the audit log.
	audit.AddMetadata(ctx, "fields", patchedFields(patch))

	if view.Profile.MarketingConsent != profile.MarketingConsent {
		audit.AddChange(ctx, "marketing_consent", view.Profile.MarketingConsent, profile.MarketingConsent)
	}

	s.invalidate(ctx)

	return s.repo.GetView(ctx, target)
}

// Public returns the profile behind ref (username or user UUID) as seen by viewer.
// Inactive accounts are hidden, and private profiles are hidden unless the viewer
// owns the profile or is privileged.
func (s *ProfileService) Public(ctx context.Context, ref string, viewer *uuid.UUID, privileged bool) (userdomain.ProfileView, error) {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return userdomain.ProfileView{}, userdomain.ErrNotFound
	}

	view, err := readcache.Through(ctx, s.cache, readcache.Users, readcache.Key("get", "ref", ref),
		func() (userdomain.ProfileView, error) {
			view, err := s.lookup(ctx, ref)
			view.User.PasswordHash = ""

			return view, err
		})
	if err != nil {
		return userdomain.ProfileView{}, err
	}

	owner := viewer != nil && *viewer == view.User.UUID
	if !view.User.IsActive() || (view.Privacy.Visibility == userdomain.VisibilityPrivate && !owner && !privileged) {
		return userdomain.ProfileView{}, userdomain.ErrNotFound
	}

	return view, nil
}

func (s *ProfileService) lookup(ctx context.Context, ref string) (userdomain.ProfileView, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return s.repo.GetView(ctx, id)
	}

	return s.repo.GetViewByUsername(ctx, ref)
}

// UploadAvatar stores a validated image as the user's avatar and removes the avatar it replaces.
func (s *ProfileService) UploadAvatar(ctx context.Context, userID uuid.UUID, filename string, data []byte) (mediadomain.MediaAsset, error) {
	if s.avatars == nil {
		return mediadomain.MediaAsset{}, ErrAvatarsUnavailable
	}

	view, err := s.repo.GetView(ctx, userID)
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	asset, err := s.avatars.UploadImage(ctx, mediadomain.ImageUpload{
		UploadedBy: &userID,
		Filename:   filename,
		Data:       data,
		MaxBytes:   s.avatarMaxBytes,
		Tags:       []string{AvatarTag},
	})
	if err != nil {
		return mediadomain.MediaAsset{}, err
	}

	if err := s.repo.SetAvatar(ctx, userID, &asset.UUID, s.clock.Now()); err != nil {
		return mediadomain.MediaAsset{}, err
	}

	s.invalidate(ctx)

	// The new avatar is already live; a failed cleanup only leaves an orphaned asset.
	_ = s.removeOwnedAvatar(ctx, userID, view.AvatarUUID)

	return asset, nil
}

// DeleteAvatar clears the user's avatar; it succeeds when there is none.
func (s *ProfileService) DeleteAvatar(ctx context.Context, userID uuid.UUID) error {
	if s.avatars == nil {
		return ErrAvatarsUnavailable
	}

	view, err := s.repo.GetView(ctx, userID)
	if err != nil {
		return err
	}

	if view.AvatarUUID == nil {
		return nil
	}

	if err := s.repo.SetAvatar(ctx, userID, nil, s.clock.Now()); err != nil {
		return err
	}

	s.invalidate(ctx)

	return s.removeOwnedAvatar(ctx, userID, view.AvatarUUID)
}

// removeOwnedAvatar deletes an avatar asset the user uploaded as an avatar; assets
// assigned any other way are left alone.
func (s *ProfileService) removeOwnedAvatar(ctx context.Context, userID uuid.UUID, avatarID *uuid.UUID) error {
	if avatarID == nil {
		return nil
	}

	asset, err := s.avatars.Get(ctx, *avatarID)
	if errors.Is(err, mediadomain.ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if asset.UploadedByUUID == nil || *asset.UploadedByUUID != userID || !slices.Contains(asset.Tags, AvatarTag) {
		return nil
	}

	return s.avatars.Delete(ctx, asset.UUID)
}

func (s *ProfileService) invalidate(ctx context.Context) {
	readcache.Invalidate(ctx, s.cache, readcache.Users)
}

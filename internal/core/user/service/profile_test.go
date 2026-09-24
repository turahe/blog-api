package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	"github.com/turahe/blog-api/internal/core/readcache"
	"github.com/turahe/blog-api/internal/core/readcache/readcachetest"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type fakeProfileRepo struct {
	views    map[uuid.UUID]userdomain.ProfileView
	saveErr  error
	saved    int
	getCalls int
}

func newFakeProfileRepo(views ...userdomain.ProfileView) *fakeProfileRepo {
	repo := &fakeProfileRepo{views: map[uuid.UUID]userdomain.ProfileView{}}
	for _, view := range views {
		repo.views[view.User.UUID] = view
	}

	return repo
}

func (r *fakeProfileRepo) GetView(_ context.Context, id uuid.UUID) (userdomain.ProfileView, error) {
	r.getCalls++

	view, ok := r.views[id]
	if !ok || view.User.DeletedAt != nil {
		return userdomain.ProfileView{}, userdomain.ErrNotFound
	}

	return view, nil
}

func (r *fakeProfileRepo) GetViewByUsername(ctx context.Context, username string) (userdomain.ProfileView, error) {
	for id, view := range r.views {
		if strings.EqualFold(view.User.Username, username) {
			return r.GetView(ctx, id)
		}
	}

	return userdomain.ProfileView{}, userdomain.ErrNotFound
}

func (r *fakeProfileRepo) SaveProfile(_ context.Context, id uuid.UUID, fullName *string, profile userdomain.Profile, _ uuid.UUID, at time.Time) error {
	if r.saveErr != nil {
		return r.saveErr
	}

	r.saved++
	view := r.views[id]
	view.Profile = profile
	view.Profile.UpdatedAt = &at

	if fullName != nil {
		view.User.FullName = *fullName
	}

	r.views[id] = view

	return nil
}

func (r *fakeProfileRepo) SetAvatar(_ context.Context, id uuid.UUID, mediaID *uuid.UUID, _ time.Time) error {
	view, ok := r.views[id]
	if !ok {
		return userdomain.ErrNotFound
	}

	view.AvatarUUID = mediaID
	r.views[id] = view

	return nil
}

type fakeAvatarStore struct {
	assets    map[uuid.UUID]mediadomain.MediaAsset
	uploadErr error
	deleted   []uuid.UUID
	lastInput mediadomain.ImageUpload
}

func newFakeAvatarStore(assets ...mediadomain.MediaAsset) *fakeAvatarStore {
	store := &fakeAvatarStore{assets: map[uuid.UUID]mediadomain.MediaAsset{}}
	for _, asset := range assets {
		store.assets[asset.UUID] = asset
	}

	return store
}

func (s *fakeAvatarStore) UploadImage(_ context.Context, input mediadomain.ImageUpload) (mediadomain.MediaAsset, error) {
	s.lastInput = input
	if s.uploadErr != nil {
		return mediadomain.MediaAsset{}, s.uploadErr
	}

	asset := mediadomain.MediaAsset{UUID: uuid.New(), UploadedByUUID: input.UploadedBy, Tags: input.Tags, Status: mediadomain.StatusReady}
	s.assets[asset.UUID] = asset

	return asset, nil
}

func (s *fakeAvatarStore) Get(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	asset, ok := s.assets[id]
	if !ok {
		return mediadomain.MediaAsset{}, mediadomain.ErrNotFound
	}

	return asset, nil
}

func (s *fakeAvatarStore) Delete(_ context.Context, id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	delete(s.assets, id)

	return nil
}

func activeView(username string) userdomain.ProfileView {
	return userdomain.ProfileView{
		User: userdomain.User{
			UUID: uuid.New(), Username: username, Email: username + "@example.com", FullName: "Full " + username,
			PasswordHash: "secret-hash", Status: userdomain.StatusActive,
		},
		Profile: userdomain.DefaultProfile(),
		Privacy: userdomain.DefaultPrivacy(),
	}
}

func set(s string) userdomain.Change[string] { return userdomain.Change[string]{Set: true, Value: &s} }

var cleared = userdomain.Change[string]{Set: true}

func TestApplyPatchValidatesFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		patch userdomain.ProfilePatch
		ok    bool
	}{
		"full name":             {userdomain.ProfilePatch{FullName: set("Ada Lovelace")}, true},
		"null full name":        {userdomain.ProfilePatch{FullName: cleared}, false},
		"blank full name":       {userdomain.ProfilePatch{FullName: set("   ")}, false},
		"display name":          {userdomain.ProfilePatch{DisplayName: set("Ada L.")}, true},
		"display name symbols":  {userdomain.ProfilePatch{DisplayName: set("<script>")}, false},
		"display name too long": {userdomain.ProfilePatch{DisplayName: set(strings.Repeat("a", 61))}, false},
		"bio multiline":         {userdomain.ProfilePatch{Bio: set("line one\nline two")}, true},
		"bio control char":      {userdomain.ProfilePatch{Bio: set("bad\x00byte")}, false},
		"bio too long":          {userdomain.ProfilePatch{Bio: set(strings.Repeat("é", 4001))}, false},
		"website":               {userdomain.ProfilePatch{ContactWebsite: set("https://ada.dev/about")}, true},
		"website javascript":    {userdomain.ProfilePatch{ContactWebsite: set("javascript:alert(1)")}, false},
		"website credentials":   {userdomain.ProfilePatch{ContactWebsite: set("https://u:p@ada.dev")}, false},
		"website relative":      {userdomain.ProfilePatch{ContactWebsite: set("/about")}, false},
		"twitter with at":       {userdomain.ProfilePatch{Twitter: set("@ada_l")}, true},
		"twitter too long":      {userdomain.ProfilePatch{Twitter: set("abcdefghijklmnop")}, false},
		"github":                {userdomain.ProfilePatch{GitHub: set("ada-lovelace")}, true},
		"github url":            {userdomain.ProfilePatch{GitHub: set("https://github.com/ada")}, false},
		"linkedin":              {userdomain.ProfilePatch{LinkedIn: set("ada-lovelace-1815")}, true},
		"locale":                {userdomain.ProfilePatch{Locale: set("id_ID")}, true},
		"locale dash":           {userdomain.ProfilePatch{Locale: set("en-GB")}, true},
		"locale bogus":          {userdomain.ProfilePatch{Locale: set("english")}, false},
		"null locale":           {userdomain.ProfilePatch{Locale: cleared}, false},
		"timezone":              {userdomain.ProfilePatch{Timezone: set("Asia/Jakarta")}, true},
		"timezone local":        {userdomain.ProfilePatch{Timezone: set("Local")}, false},
		"timezone bogus":        {userdomain.ProfilePatch{Timezone: set("Mars/Olympus")}, false},
		"null consent":          {userdomain.ProfilePatch{MarketingConsent: userdomain.Change[bool]{Set: true}}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			profile := userdomain.DefaultProfile()

			_, err := applyPatch(&profile, tc.patch, now)
			if tc.ok {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, userdomain.ErrProfileValidation)
		})
	}
}

func TestApplyPatchNormalizesAndClears(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	profile := userdomain.DefaultProfile()
	profile.DisplayName = new("Old")
	profile.Social.GitHub = "old"

	consent := true
	fullName, err := applyPatch(&profile, userdomain.ProfilePatch{
		FullName:         set("  Ada  "),
		DisplayName:      cleared,
		GitHub:           set(""),
		Twitter:          set("@ada"),
		Locale:           set("en-GB"),
		MarketingConsent: userdomain.Change[bool]{Set: true, Value: &consent},
	}, now)
	require.NoError(t, err)
	require.Equal(t, "Ada", *fullName)
	require.Nil(t, profile.DisplayName)
	require.Empty(t, profile.Social.GitHub)
	require.Equal(t, "ada", profile.Social.Twitter)
	require.Equal(t, "en_GB", profile.Locale)
	require.True(t, profile.MarketingConsent)
	require.Equal(t, now, *profile.MarketingConsentUpdatedAt)

	later := now.Add(time.Hour)
	_, err = applyPatch(&profile, userdomain.ProfilePatch{MarketingConsent: userdomain.Change[bool]{Set: true, Value: &consent}}, later)
	require.NoError(t, err)
	require.Equal(t, now, *profile.MarketingConsentUpdatedAt, "unchanged consent keeps its timestamp")
}

func TestUpdateSavesInvalidatesAndReturnsFreshView(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	repo := newFakeProfileRepo(view)
	cache := readcachetest.New()
	svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithCache(cache)

	got, err := svc.Update(t.Context(), view.User.UUID, view.User.UUID, userdomain.ProfilePatch{Bio: set("Hello")})
	require.NoError(t, err)
	require.Equal(t, "Hello", got.Profile.Bio)
	require.Equal(t, 1, cache.Invalidations(readcache.Users))

	_, err = svc.Update(t.Context(), view.User.UUID, view.User.UUID, userdomain.ProfilePatch{})
	require.ErrorIs(t, err, ErrEmptyPatch)

	_, err = svc.Update(t.Context(), view.User.UUID, uuid.New(), userdomain.ProfilePatch{Bio: set("x")})
	require.ErrorIs(t, err, userdomain.ErrNotFound)

	repo.saveErr = userdomain.ErrDisplayNameTaken
	_, err = svc.Update(t.Context(), view.User.UUID, view.User.UUID, userdomain.ProfilePatch{DisplayName: set("Taken")})
	require.ErrorIs(t, err, userdomain.ErrDisplayNameTaken)
	require.Equal(t, 1, cache.Invalidations(readcache.Users), "failed writes do not invalidate")
}

func TestPublicHonoursVisibilityAndStatus(t *testing.T) {
	t.Parallel()

	public := activeView("pub")
	private := activeView("priv")
	private.Privacy.Visibility = userdomain.VisibilityPrivate
	suspended := activeView("gone")
	suspended.User.Status = userdomain.StatusSuspended

	svc := NewProfileService(newFakeProfileRepo(public, private, suspended), fixedClock{t: time.Now()})
	owner, stranger := private.User.UUID, uuid.New()

	got, err := svc.Public(t.Context(), "PUB", nil, false)
	require.NoError(t, err)
	require.Equal(t, public.User.UUID, got.User.UUID)

	got, err = svc.Public(t.Context(), public.User.UUID.String(), nil, false)
	require.NoError(t, err)
	require.Equal(t, "pub", got.User.Username)

	_, err = svc.Public(t.Context(), "priv", nil, false)
	require.ErrorIs(t, err, userdomain.ErrNotFound)

	_, err = svc.Public(t.Context(), "priv", &stranger, false)
	require.ErrorIs(t, err, userdomain.ErrNotFound)

	_, err = svc.Public(t.Context(), "priv", &owner, false)
	require.NoError(t, err)

	_, err = svc.Public(t.Context(), "priv", &stranger, true)
	require.NoError(t, err)

	_, err = svc.Public(t.Context(), "gone", nil, true)
	require.ErrorIs(t, err, userdomain.ErrNotFound)

	_, err = svc.Public(t.Context(), "  ", nil, false)
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

func TestPublicCachesWithoutPasswordHash(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	repo := newFakeProfileRepo(view)
	cache := readcachetest.New()
	svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithCache(cache)

	first, err := svc.Public(t.Context(), "ada", nil, false)
	require.NoError(t, err)
	require.Empty(t, first.User.PasswordHash)

	second, err := svc.Public(t.Context(), "Ada", nil, false)
	require.NoError(t, err)
	require.Equal(t, first.User.UUID, second.User.UUID)
	require.Equal(t, 1, repo.getCalls, "second read is served from cache")
	require.Len(t, cache.Keys(readcache.Users), 1)
}

func TestUploadAvatarReplacesOwnedAvatarOnly(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	userID := view.User.UUID
	owned := mediadomain.MediaAsset{UUID: uuid.New(), UploadedByUUID: &userID, Tags: []string{AvatarTag}}
	view.AvatarUUID = &owned.UUID

	repo := newFakeProfileRepo(view)
	store := newFakeAvatarStore(owned)
	cache := readcachetest.New()
	svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithAvatars(store, 1024).WithCache(cache)

	asset, err := svc.UploadAvatar(t.Context(), userID, "me.png", []byte("img"))
	require.NoError(t, err)
	require.Equal(t, asset.UUID, *repo.views[userID].AvatarUUID)
	require.Equal(t, []uuid.UUID{owned.UUID}, store.deleted)
	require.Equal(t, int64(1024), store.lastInput.MaxBytes)
	require.Equal(t, []string{AvatarTag}, store.lastInput.Tags)
	require.Equal(t, 1, cache.Invalidations(readcache.Users))

	editor := uuid.New()
	shared := mediadomain.MediaAsset{UUID: uuid.New(), UploadedByUUID: &editor, Tags: []string{AvatarTag}}
	store.assets[shared.UUID] = shared
	view = repo.views[userID]
	view.AvatarUUID = &shared.UUID
	repo.views[userID] = view

	_, err = svc.UploadAvatar(t.Context(), userID, "me.png", []byte("img"))
	require.NoError(t, err)
	require.Len(t, store.deleted, 1, "an avatar uploaded by someone else is kept")
}

func TestUploadAvatarErrors(t *testing.T) {
	t.Parallel()

	view := activeView("ada")

	_, err := NewProfileService(newFakeProfileRepo(view), fixedClock{t: time.Now()}).
		UploadAvatar(t.Context(), view.User.UUID, "a.png", []byte("x"))
	require.ErrorIs(t, err, ErrAvatarsUnavailable)

	store := newFakeAvatarStore()
	store.uploadErr = errors.New("bad image")
	repo := newFakeProfileRepo(view)
	svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithAvatars(store, 10)

	_, err = svc.UploadAvatar(t.Context(), view.User.UUID, "a.png", []byte("x"))
	require.ErrorContains(t, err, "bad image")
	require.Nil(t, repo.views[view.User.UUID].AvatarUUID)

	_, err = svc.UploadAvatar(t.Context(), uuid.New(), "a.png", []byte("x"))
	require.ErrorIs(t, err, userdomain.ErrNotFound)
}

func TestDeleteAvatar(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	userID := view.User.UUID
	repo := newFakeProfileRepo(view)
	store := newFakeAvatarStore()
	svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithAvatars(store, 10)

	require.NoError(t, svc.DeleteAvatar(t.Context(), userID), "no avatar is a no-op")
	require.Empty(t, store.deleted)

	owned := mediadomain.MediaAsset{UUID: uuid.New(), UploadedByUUID: &userID, Tags: []string{AvatarTag}}
	store.assets[owned.UUID] = owned
	view.AvatarUUID = &owned.UUID
	repo.views[userID] = view

	require.NoError(t, svc.DeleteAvatar(t.Context(), userID))
	require.Nil(t, repo.views[userID].AvatarUUID)
	require.Equal(t, []uuid.UUID{owned.UUID}, store.deleted)
}

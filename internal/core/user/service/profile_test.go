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
	views        map[uuid.UUID]userdomain.ProfileView
	saveErr      error
	setAvatarErr error
	saved        int
	getCalls     int
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
	if r.setAvatarErr != nil {
		return r.setAvatarErr
	}

	view, ok := r.views[id]
	if !ok {
		return userdomain.ErrNotFound
	}

	view.AvatarUUID = mediaID
	r.views[id] = view

	return nil
}

func (r *fakeProfileRepo) SavePrivacy(_ context.Context, id uuid.UUID, privacy userdomain.Privacy, _ time.Time) error {
	if r.saveErr != nil {
		return r.saveErr
	}

	r.saved++
	view := r.views[id]
	view.Privacy = privacy
	r.views[id] = view

	return nil
}

type fakeAvatarStore struct {
	assets    map[uuid.UUID]mediadomain.MediaAsset
	uploadErr error
	getErr    error
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
	if s.getErr != nil {
		return mediadomain.MediaAsset{}, s.getErr
	}

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
		"null bio":              {userdomain.ProfilePatch{Bio: cleared}, true},
		"location":              {userdomain.ProfilePatch{ContactLocation: set("Jakarta, Indonesia")}, true},
		"location too long":     {userdomain.ProfilePatch{ContactLocation: set(strings.Repeat("l", maxLocationRunes+1))}, false},
		"location newline":      {userdomain.ProfilePatch{ContactLocation: set("Jakarta\nIndonesia")}, false},
		"website":               {userdomain.ProfilePatch{ContactWebsite: set("https://ada.dev/about")}, true},
		"website javascript":    {userdomain.ProfilePatch{ContactWebsite: set("javascript:alert(1)")}, false},
		"website credentials":   {userdomain.ProfilePatch{ContactWebsite: set("https://u:p@ada.dev")}, false},
		"website relative":      {userdomain.ProfilePatch{ContactWebsite: set("/about")}, false},
		"twitter with at":       {userdomain.ProfilePatch{Twitter: set("@ada_l")}, true},
		"twitter too long":      {userdomain.ProfilePatch{Twitter: set("abcdefghijklmnop")}, false},
		"null twitter":          {userdomain.ProfilePatch{Twitter: cleared}, true},
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

func TestGetReturnsTheFullView(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	svc := NewProfileService(newFakeProfileRepo(view), fixedClock{t: time.Now()})

	got, err := svc.Get(t.Context(), view.User.UUID)
	require.NoError(t, err)
	require.Equal(t, view, got)

	_, err = svc.Get(t.Context(), uuid.New())
	require.ErrorIs(t, err, userdomain.ErrNotFound)
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

func TestUpdateRejectsInvalidPatchWithoutSaving(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	repo := newFakeProfileRepo(view)
	svc := NewProfileService(repo, fixedClock{t: time.Now()})

	_, err := svc.Update(t.Context(), view.User.UUID, view.User.UUID, userdomain.ProfilePatch{Locale: set("english")})
	require.ErrorIs(t, err, userdomain.ErrProfileValidation)
	require.Zero(t, repo.saved)
}

func TestUpdateChangesMarketingConsent(t *testing.T) {
	t.Parallel()

	view := activeView("ada")
	svc := NewProfileService(newFakeProfileRepo(view), fixedClock{t: time.Now()})

	got, err := svc.Update(t.Context(), view.User.UUID, view.User.UUID,
		userdomain.ProfilePatch{MarketingConsent: userdomain.Change[bool]{Set: true, Value: new(!view.Profile.MarketingConsent)}})
	require.NoError(t, err)
	require.NotEqual(t, view.Profile.MarketingConsent, got.Profile.MarketingConsent)
	require.NotNil(t, got.Profile.MarketingConsentUpdatedAt)
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

	_, err = svc.Public(t.Context(), "nobody", nil, true)
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

func TestUploadAvatarFirstAvatarAndStaleReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		previous *uuid.UUID
	}{
		{name: "no previous avatar"},
		{name: "previous asset already gone", previous: new(uuid.New())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			view := activeView("ada")
			view.AvatarUUID = tt.previous
			repo := newFakeProfileRepo(view)
			store := newFakeAvatarStore()
			svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithAvatars(store, 10)

			asset, err := svc.UploadAvatar(t.Context(), view.User.UUID, "a.png", []byte("x"))
			require.NoError(t, err)
			require.Equal(t, &asset.UUID, repo.views[view.User.UUID].AvatarUUID)
			require.Empty(t, store.deleted)
		})
	}
}

func TestUploadAvatarFailsWhenTheReferenceCannotBeSaved(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	view := activeView("ada")
	repo := newFakeProfileRepo(view)
	repo.setAvatarErr = boom
	svc := NewProfileService(repo, fixedClock{t: time.Now()}).WithAvatars(newFakeAvatarStore(), 10)

	_, err := svc.UploadAvatar(t.Context(), view.User.UUID, "a.png", []byte("x"))
	require.ErrorIs(t, err, boom)
	require.Nil(t, repo.views[view.User.UUID].AvatarUUID)
}

func TestDeleteAvatarFailures(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name     string
		avatars  bool
		user     func(view userdomain.ProfileView) uuid.UUID
		breakIt  func(*fakeProfileRepo, *fakeAvatarStore)
		wantErr  error
		unlinked bool
	}{
		{
			name:    "avatars not configured",
			user:    func(v userdomain.ProfileView) uuid.UUID { return v.User.UUID },
			breakIt: func(*fakeProfileRepo, *fakeAvatarStore) {},
			wantErr: ErrAvatarsUnavailable,
		},
		{
			name:    "unknown user",
			avatars: true,
			user:    func(userdomain.ProfileView) uuid.UUID { return uuid.New() },
			breakIt: func(*fakeProfileRepo, *fakeAvatarStore) {},
			wantErr: userdomain.ErrNotFound,
		},
		{
			name:    "reference cannot be cleared",
			avatars: true,
			user:    func(v userdomain.ProfileView) uuid.UUID { return v.User.UUID },
			breakIt: func(r *fakeProfileRepo, _ *fakeAvatarStore) { r.setAvatarErr = boom },
			wantErr: boom,
		},
		{
			name:     "asset lookup fails after unlinking",
			avatars:  true,
			user:     func(v userdomain.ProfileView) uuid.UUID { return v.User.UUID },
			breakIt:  func(_ *fakeProfileRepo, s *fakeAvatarStore) { s.getErr = boom },
			wantErr:  boom,
			unlinked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			view := activeView("ada")
			userID := view.User.UUID
			owned := mediadomain.MediaAsset{UUID: uuid.New(), UploadedByUUID: &userID, Tags: []string{AvatarTag}}
			view.AvatarUUID = &owned.UUID
			repo := newFakeProfileRepo(view)
			store := newFakeAvatarStore(owned)
			tt.breakIt(repo, store)

			svc := NewProfileService(repo, fixedClock{t: time.Now()})
			if tt.avatars {
				svc = svc.WithAvatars(store, 10)
			}

			require.ErrorIs(t, svc.DeleteAvatar(t.Context(), tt.user(view)), tt.wantErr)
			require.Empty(t, store.deleted)

			if tt.unlinked {
				require.Nil(t, repo.views[userID].AvatarUUID)
			} else {
				require.Equal(t, &owned.UUID, repo.views[userID].AvatarUUID)
			}
		})
	}
}

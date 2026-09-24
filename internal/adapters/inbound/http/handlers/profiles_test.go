package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
	userservice "github.com/turahe/blog-api/internal/core/user/service"
)

type fakeProfiles struct {
	view        userdomain.ProfileView
	err         error
	patch       userdomain.ProfilePatch
	actor       uuid.UUID
	target      uuid.UUID
	ref         string
	viewer      *uuid.UUID
	privileged  bool
	uploadBytes []byte
	uploadName  string
}

func (f *fakeProfiles) Get(_ context.Context, id uuid.UUID) (userdomain.ProfileView, error) {
	f.target = id
	return f.view, f.err
}

func (f *fakeProfiles) Update(_ context.Context, actor, target uuid.UUID, patch userdomain.ProfilePatch) (userdomain.ProfileView, error) {
	f.actor, f.target, f.patch = actor, target, patch
	return f.view, f.err
}

func (f *fakeProfiles) Public(_ context.Context, ref string, viewer *uuid.UUID, privileged bool) (userdomain.ProfileView, error) {
	f.ref, f.viewer, f.privileged = ref, viewer, privileged
	return f.view, f.err
}

func (f *fakeProfiles) UploadAvatar(_ context.Context, _ uuid.UUID, filename string, data []byte) (mediadomain.MediaAsset, error) {
	f.uploadName, f.uploadBytes = filename, data
	if f.err != nil {
		return mediadomain.MediaAsset{}, f.err
	}

	return mediadomain.MediaAsset{UUID: uuid.New(), ContentType: "image/png", Status: mediadomain.StatusReady}, nil
}

func (f *fakeProfiles) DeleteAvatar(context.Context, uuid.UUID) error { return f.err }

func sampleView() userdomain.ProfileView {
	website := "https://ada.dev"

	view := userdomain.ProfileView{
		User: userdomain.User{
			UUID: uuid.New(), Username: "ada", Email: "ada@example.com", FullName: "Ada",
			Status: userdomain.StatusActive, CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		Profile: userdomain.DefaultProfile(),
		Privacy: userdomain.DefaultPrivacy(),
	}
	view.Profile.ContactWebsite = &website

	return view
}

type profileRequest struct {
	method, target, body, contentType string
	param                             string
	user                              *uuid.UUID
}

func runProfile(t *testing.T, handler gin.HandlerFunc, req profileRequest) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), req.method, req.target, strings.NewReader(req.body))

	if req.contentType != "" {
		c.Request.Header.Set("Content-Type", req.contentType)
	}

	if req.param != "" {
		c.Params = gin.Params{{Key: "param1", Value: req.param}}
	}

	if req.user != nil {
		c.Set(middleware.ContextUserIDKey, *req.user)
	}

	handler(c)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())

	return w, body
}

func errorCode(body map[string]any) string {
	errObj, _ := body["error"].(map[string]any)
	code, _ := errObj["code"].(string)

	return code
}

func dataOf(body map[string]any) map[string]any {
	data, _ := body["data"].(map[string]any)
	return data
}

func TestMePatchProfileDecodesAllowlistedFields(t *testing.T) {
	t.Parallel()

	profiles := &fakeProfiles{view: sampleView()}
	user := uuid.New()

	w, body := runProfile(t, mePatchProfileHandler(profiles), profileRequest{
		method: nethttp.MethodPatch, target: "/api/v1/me/profile", user: &user,
		body: `{"display_name":null,"bio":"hi","social_links":{"github":"ada"},"marketing_consent":true}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, user, profiles.actor)
	require.Equal(t, user, profiles.target)

	patch := profiles.patch
	require.True(t, patch.DisplayName.Set)
	require.Nil(t, patch.DisplayName.Value)
	require.Equal(t, "hi", *patch.Bio.Value)
	require.Equal(t, "ada", *patch.GitHub.Value)
	require.False(t, patch.Twitter.Set)
	require.True(t, *patch.MarketingConsent.Value)
	require.False(t, patch.FullName.Set)

	data := dataOf(body)
	require.Equal(t, "ada@example.com", data["email"], "the owner sees their email")
	require.Contains(t, data, "privacy")
}

func TestMePatchProfileRejectsBadBodies(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		code string
	}{
		"unknown key":        {`{"is_admin":true}`, "profile.unknown_field"},
		"unknown social key": {`{"social_links":{"myspace":"x"}}`, "profile.unknown_field"},
		"unsupported phone":  {`{"contact_phone":"+62"}`, "profile.field_unsupported"},
		"wrong type":         {`{"bio":42}`, responses.ErrorCodeValidation},
		"not an object":      {`[1,2]`, responses.ErrorCodeValidation},
		"malformed":          {`{"bio":`, responses.ErrorCodeValidation},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			profiles := &fakeProfiles{view: sampleView()}
			user := uuid.New()

			w, body := runProfile(t, mePatchProfileHandler(profiles), profileRequest{
				method: nethttp.MethodPatch, target: "/api/v1/me/profile", user: &user, body: tc.body,
			})
			require.Equal(t, nethttp.StatusBadRequest, w.Code)
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, uuid.Nil, profiles.target, "the service is not called")
		})
	}
}

func TestProfileErrorMapping(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"validation":   {userdomain.ErrProfileValidation, nethttp.StatusBadRequest, responses.ErrorCodeValidation},
		"empty patch":  {userservice.ErrEmptyPatch, nethttp.StatusBadRequest, responses.ErrorCodeValidation},
		"name taken":   {userdomain.ErrDisplayNameTaken, nethttp.StatusConflict, "profile.display_name_taken"},
		"self missing": {userdomain.ErrNotFound, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized},
		"storage down": {mediaservice.ErrStorage, nethttp.StatusBadGateway, "storage_unavailable"},
		"internal":     {errors.New("boom"), nethttp.StatusInternalServerError, responses.ErrorCodeInternal},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			user := uuid.New()
			w, body := runProfile(t, mePatchProfileHandler(&fakeProfiles{err: tc.err}), profileRequest{
				method: nethttp.MethodPatch, target: "/api/v1/me/profile", user: &user, body: `{"bio":"x"}`,
			})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestAdminProfileHandlers(t *testing.T) {
	t.Parallel()

	view := sampleView()
	profiles := &fakeProfiles{view: view}
	admin := uuid.New()

	w, body := runProfile(t, adminGetProfileHandler(profiles), profileRequest{
		method: nethttp.MethodGet, target: "/api/v1/admin/users/x/profile", param: view.User.UUID.String(), user: &admin,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, "active", dataOf(body)["status"])
	require.Equal(t, view.User.UUID, profiles.target)

	w, _ = runProfile(t, adminPatchProfileHandler(profiles), profileRequest{
		method: nethttp.MethodPatch, target: "/api/v1/admin/users/x/profile", param: view.User.UUID.String(),
		user: &admin, body: `{"full_name":"Ada King"}`,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, admin, profiles.actor, "the admin is recorded as the editor")
	require.Equal(t, view.User.UUID, profiles.target)

	w, body = runProfile(t, adminGetProfileHandler(profiles), profileRequest{
		method: nethttp.MethodGet, target: "/api/v1/admin/users/x/profile", param: "not-a-uuid", user: &admin,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, responses.ErrorCodeValidation, errorCode(body))

	w, _ = runProfile(t, adminGetProfileHandler(&fakeProfiles{err: userdomain.ErrNotFound}), profileRequest{
		method: nethttp.MethodGet, target: "/api/v1/admin/users/x/profile", param: uuid.NewString(), user: &admin,
	})
	require.Equal(t, nethttp.StatusNotFound, w.Code)
}

func TestPublicUserProfile(t *testing.T) {
	t.Parallel()

	view := sampleView()
	profiles := &fakeProfiles{view: view}
	viewer := uuid.New()
	privileged := func(*gin.Context) bool { return true }

	w, body := runProfile(t, publicUserProfileHandler(profiles, privileged), profileRequest{
		method: nethttp.MethodGet, target: "/api/v1/users/ada", param: "ada",
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Empty(t, w.Header().Get("X-Robots-Tag"))
	require.Equal(t, "ada", profiles.ref)
	require.Nil(t, profiles.viewer)
	require.False(t, profiles.privileged, "anonymous callers are never privileged")

	data := dataOf(body)
	require.NotContains(t, data, "email", "email is hidden by default")
	require.NotContains(t, data, "privacy")
	require.Contains(t, data, "contact")

	profiles.view.Privacy.AllowIndexing = false
	profiles.view.Privacy.ShowContact = false
	profiles.view.Privacy.ShowEmail = true

	w, body = runProfile(t, publicUserProfileHandler(profiles, privileged), profileRequest{
		method: nethttp.MethodGet, target: "/api/v1/users/ada", param: "ada", user: &viewer,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, "noindex", w.Header().Get("X-Robots-Tag"))
	require.Equal(t, &viewer, profiles.viewer)
	require.True(t, profiles.privileged)
	require.Equal(t, "ada@example.com", dataOf(body)["email"])
	require.NotContains(t, dataOf(body), "contact")

	w, body = runProfile(t, publicUserProfileHandler(&fakeProfiles{err: userdomain.ErrNotFound}, privileged), profileRequest{
		method: nethttp.MethodGet, target: "/api/v1/users/ghost", param: "ghost",
	})
	require.Equal(t, nethttp.StatusNotFound, w.Code)
	require.Equal(t, responses.ErrorCodeNotFound, errorCode(body))
}

func multipartBody(t *testing.T, field, filename string, content []byte) (string, string) {
	t.Helper()

	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(field, filename)
	require.NoError(t, err)

	_, err = io.Copy(part, bytes.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return buf.String(), writer.FormDataContentType()
}

func TestMeUploadAvatar(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	profiles := &fakeProfiles{}
	body, contentType := multipartBody(t, "file", "me.png", []byte("png-bytes"))

	w, resp := runProfile(t, meUploadAvatarHandler(profiles, 1024), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/avatar", user: &user, body: body, contentType: contentType,
	})
	require.Equal(t, nethttp.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "me.png", profiles.uploadName)
	require.Equal(t, []byte("png-bytes"), profiles.uploadBytes)
	require.Contains(t, dataOf(resp), "avatar")

	body, contentType = multipartBody(t, "file", "big.png", bytes.Repeat([]byte("x"), 2048))
	w, resp = runProfile(t, meUploadAvatarHandler(profiles, 1024), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/avatar", user: &user, body: body, contentType: contentType,
	})
	require.Equal(t, nethttp.StatusRequestEntityTooLarge, w.Code)
	require.Equal(t, "profile.avatar_too_large", errorCode(resp))

	body, contentType = multipartBody(t, "image", "me.png", []byte("x"))
	w, resp = runProfile(t, meUploadAvatarHandler(profiles, 1024), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/avatar", user: &user, body: body, contentType: contentType,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, responses.ErrorCodeValidation, errorCode(resp))

	body, contentType = multipartBody(t, "file", "me.png", []byte("<html>"))
	w, resp = runProfile(t, meUploadAvatarHandler(&fakeProfiles{err: mediaservice.ErrValidation}, 1024), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/avatar", user: &user, body: body, contentType: contentType,
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, responses.ErrorCodeValidation, errorCode(resp))
}

func TestMeDeleteAvatar(t *testing.T) {
	t.Parallel()

	user := uuid.New()

	w, body := runProfile(t, meDeleteAvatarHandler(&fakeProfiles{}), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/me/avatar", user: &user,
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Contains(t, dataOf(body), "avatar_id")

	w, body = runProfile(t, meDeleteAvatarHandler(&fakeProfiles{}), profileRequest{
		method: nethttp.MethodDelete, target: "/api/v1/me/avatar",
	})
	require.Equal(t, nethttp.StatusUnauthorized, w.Code)
	require.Equal(t, responses.ErrorCodeUnauthorized, errorCode(body))
}

type fakeEmailChanger struct {
	err      error
	newEmail string
	password string
	token    string
}

func (f *fakeEmailChanger) RequestEmailChange(_ context.Context, _ uuid.UUID, newEmail, password string) (authdomain.EmailChangeRequest, error) {
	f.newEmail, f.password = newEmail, password
	return authdomain.EmailChangeRequest{NewEmail: newEmail, ExpiresAt: time.Now().Add(time.Hour)}, f.err
}

func (f *fakeEmailChanger) ConfirmEmailChange(_ context.Context, _ uuid.UUID, token string) (userdomain.User, error) {
	f.token = token
	now := time.Now()

	return userdomain.User{Email: "new@example.com", EmailVerifiedAt: &now}, f.err
}

func TestEmailChangeHandlers(t *testing.T) {
	t.Parallel()

	user := uuid.New()
	changer := &fakeEmailChanger{}

	w, body := runProfile(t, meRequestEmailChangeHandler(changer), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/email/request-change", user: &user,
		body: `{"new_email":"new@example.com","password_proof":"Secret123456"}`, contentType: "application/json",
	})
	require.Equal(t, nethttp.StatusAccepted, w.Code, w.Body.String())
	require.Equal(t, "Secret123456", changer.password)
	require.Equal(t, "new@example.com", dataOf(body)["new_email"])

	w, _ = runProfile(t, meRequestEmailChangeHandler(changer), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/email/request-change", user: &user,
		body: `{"new_email":"new@example.com"}`, contentType: "application/json",
	})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, "password_proof is required")

	w, body = runProfile(t, meConfirmEmailChangeHandler(changer), profileRequest{
		method: nethttp.MethodPost, target: "/api/v1/me/email/confirm-change", user: &user,
		body: `{"token":"abc"}`, contentType: "application/json",
	})
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, "abc", changer.token)
	require.Equal(t, true, dataOf(body)["sessions_revoked"])

	cases := map[error]struct {
		status int
		code   string
	}{
		authdomain.ErrEmailTaken:               {nethttp.StatusConflict, "auth.email.taken"},
		authdomain.ErrCurrentPassword:          {nethttp.StatusForbidden, "password.current_mismatch"},
		authservice.ErrEmailChangeTokenExpired: {nethttp.StatusBadRequest, "auth.email.change_token_expired"},
	}
	for err, want := range cases {
		w, body = runProfile(t, meConfirmEmailChangeHandler(&fakeEmailChanger{err: err}), profileRequest{
			method: nethttp.MethodPost, target: "/api/v1/me/email/confirm-change", user: &user,
			body: `{"token":"abc"}`, contentType: "application/json",
		})
		require.Equal(t, want.status, w.Code, err.Error())
		require.Equal(t, want.code, errorCode(body))
	}
}

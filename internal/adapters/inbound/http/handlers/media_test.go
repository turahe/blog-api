package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
)

type fakeMediaService struct {
	presignFn   func(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error)
	completeFn  func(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	transformFn func(ctx context.Context, id uuid.UUID, t mediadomain.Transform) (string, error)
}

func (f *fakeMediaService) PresignUpload(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error) {
	return f.presignFn(ctx, uploadedBy, filename, contentType, sizeBytes, tags)
}

func (f *fakeMediaService) CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	return f.completeFn(ctx, id)
}

func (f *fakeMediaService) List(context.Context, mediadomain.ListFilter) (mediadomain.ListResult, error) {
	return mediadomain.ListResult{}, nil
}

func (f *fakeMediaService) Get(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) GetReady(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) Delete(context.Context, uuid.UUID) error {
	return nil
}

func (f *fakeMediaService) UpdateTags(context.Context, uuid.UUID, []string) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) UploadImage(context.Context, mediadomain.ImageUpload) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrValidation
}

func (f *fakeMediaService) TransformURL(ctx context.Context, id uuid.UUID, t mediadomain.Transform) (string, error) {
	if f.transformFn == nil {
		return "", mediaservice.ErrTransformDisabled
	}

	return f.transformFn(ctx, id, t)
}

func serveTransform(t *testing.T, svc *fakeMediaService, target string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/api/v1/media/:param1/transform", publicTransformMediaHandler(svc))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, target, nil))

	return recorder
}

func TestMediaTransformRedirectsToSignedURL(t *testing.T) {
	t.Parallel()

	mediaID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	svc := &fakeMediaService{transformFn: func(_ context.Context, id uuid.UUID, tr mediadomain.Transform) (string, error) {
		require.Equal(t, mediaID, id)
		require.Equal(t, mediadomain.Transform{Width: 256, Format: "webp"}, tr)

		return "https://img.example.com/sig/exp:1/rs:fit:256:0/src.webp", nil
	}}

	recorder := serveTransform(t, svc, "/api/v1/media/"+mediaID.String()+"/transform?w=256&format=WEBP")
	require.Equal(t, nethttp.StatusFound, recorder.Code)
	require.Equal(t, "https://img.example.com/sig/exp:1/rs:fit:256:0/src.webp", recorder.Header().Get("Location"))
	require.Equal(t, "public, max-age=300", recorder.Header().Get("Cache-Control"))
}

func TestMediaTransformErrors(t *testing.T) {
	t.Parallel()

	id := uuid.NewString()
	validation := &fakeMediaService{transformFn: func(context.Context, uuid.UUID, mediadomain.Transform) (string, error) {
		return "", mediaservice.ErrValidation
	}}
	missing := &fakeMediaService{transformFn: func(context.Context, uuid.UUID, mediadomain.Transform) (string, error) {
		return "", mediaservice.ErrNotFound
	}}

	for name, tc := range map[string]struct {
		svc    *fakeMediaService
		target string
		status int
	}{
		"disabled":         {&fakeMediaService{}, "/api/v1/media/" + id + "/transform?w=256", nethttp.StatusNotImplemented},
		"bad id":           {validation, "/api/v1/media/nope/transform?w=256", nethttp.StatusBadRequest},
		"missing width":    {validation, "/api/v1/media/" + id + "/transform", nethttp.StatusBadRequest},
		"width not listed": {validation, "/api/v1/media/" + id + "/transform?w=257", nethttp.StatusBadRequest},
		"not found":        {missing, "/api/v1/media/" + id + "/transform?w=256", nethttp.StatusNotFound},
	} {
		require.Equal(t, tc.status, serveTransform(t, tc.svc, tc.target).Code, name)
	}
}

func TestMediaPresignHappyPath(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	mediaID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	expires := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	svc := &fakeMediaService{
		presignFn: func(_ context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, _ []string) (mediadomain.PresignResult, error) {
			require.NotNil(t, uploadedBy)
			require.Equal(t, userID, *uploadedBy)
			require.Equal(t, "photo.png", filename)
			require.Equal(t, "image/png", contentType)
			require.Equal(t, int64(1024), sizeBytes)

			return mediadomain.PresignResult{
				Asset: mediadomain.MediaAsset{
					UUID:       mediaID,
					StorageKey: "media/" + mediaID.String() + "/photo.png",
					Disk:       "minio",
					Status:     mediadomain.StatusPending,
				},
				UploadURL:       "https://storage.example/upload",
				RequiredHeaders: map[string]string{"Content-Type": "image/png"},
				ExpiresAt:       expires,
			}, nil
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"original_filename":"photo.png","content_type":"image/png","size_bytes":1024}`
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/media", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.ContextUserIDKey, userID)

	adminPresignMediaHandler(svc)(c)

	require.Equal(t, nethttp.StatusCreated, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.True(t, envelope.OK)
	data, ok := envelope.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://storage.example/upload", data["upload_url"])
	require.Equal(t, mediaID.String(), data["media_id"])
}

func TestMediaPresignValidation(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	svc := &fakeMediaService{
		presignFn: func(context.Context, *uuid.UUID, string, string, int64, []string) (mediadomain.PresignResult, error) {
			return mediadomain.PresignResult{}, mediaservice.ErrValidation
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/media", bytes.NewBufferString(`{"original_filename":"x","content_type":"text/plain","size_bytes":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.ContextUserIDKey, uuid.New())

	adminPresignMediaHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
}

func TestMediaCompleteMapsErrors(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	cases := map[string]struct {
		err    error
		status int
		code   string
	}{
		"not found":  {mediaservice.ErrNotFound, nethttp.StatusNotFound, "not_found"},
		"incomplete": {mediaservice.ErrUploadIncomplete, nethttp.StatusConflict, "media.upload_incomplete"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			id := uuid.New()
			svc := &fakeMediaService{
				completeFn: func(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
					return mediadomain.MediaAsset{}, tc.err
				},
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "param1", Value: id.String()}}
			c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/media/"+id.String()+"/complete", nil)

			adminCompleteMediaHandler(svc)(c)

			require.Equal(t, tc.status, w.Code)

			var envelope responses.Envelope
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Equal(t, tc.code, envelope.Error.Code)
		})
	}
}

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
	mediaservice "github.com/turahe/blog-api/internal/core/media/service"
)

type fakeMediaService struct {
	presignFn  func(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error)
	completeFn func(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
}

func (f *fakeMediaService) PresignUpload(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error) {
	return f.presignFn(ctx, uploadedBy, filename, contentType, sizeBytes, tags)
}

func (f *fakeMediaService) CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	return f.completeFn(ctx, id)
}

func (f *fakeMediaService) List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	return mediadomain.ListResult{}, nil
}

func (f *fakeMediaService) Get(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) GetReady(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) Delete(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeMediaService) UpdateTags(ctx context.Context, id uuid.UUID, tags []string) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func TestMediaPresignHappyPath(t *testing.T) {
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
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/media", bytes.NewBufferString(body))
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
	gin.SetMode(gin.TestMode)
	svc := &fakeMediaService{
		presignFn: func(context.Context, *uuid.UUID, string, string, int64, []string) (mediadomain.PresignResult, error) {
			return mediadomain.PresignResult{}, mediaservice.ErrValidation
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/media", bytes.NewBufferString(`{"original_filename":"x","content_type":"text/plain","size_bytes":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.ContextUserIDKey, uuid.New())

	adminPresignMediaHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
}

func TestMediaCompleteNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	svc := &fakeMediaService{
		completeFn: func(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
			return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: id.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/media/"+id.String()+"/complete", nil)

	adminCompleteMediaHandler(svc)(c)

	require.Equal(t, nethttp.StatusNotFound, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "not_found", envelope.Error.Code)
}

func TestMediaCompleteIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	svc := &fakeMediaService{
		completeFn: func(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
			return mediadomain.MediaAsset{}, mediaservice.ErrUploadIncomplete
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "param1", Value: id.String()}}
	c.Request = httptest.NewRequest(nethttp.MethodPost, "/api/v1/admin/media/"+id.String()+"/complete", nil)

	adminCompleteMediaHandler(svc)(c)

	require.Equal(t, nethttp.StatusConflict, w.Code)
	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "media.upload_incomplete", envelope.Error.Code)
}

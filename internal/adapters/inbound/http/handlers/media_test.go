package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/turahe/blog-api/internal/shared/pagination"
)

type fakeMediaService struct {
	presignFn   func(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error)
	completeFn  func(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	transformFn func(ctx context.Context, id uuid.UUID, t mediadomain.Transform) (string, error)
	usageFn     func(ctx context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error)
	listFn      func(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error)
	getReadyFn  func(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error)
	deleteFn    func(ctx context.Context, id uuid.UUID) error
	tagsFn      func(ctx context.Context, id uuid.UUID, tags []string) (mediadomain.MediaAsset, error)
	variantsFn  func(ctx context.Context, assets ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error)
}

func (f *fakeMediaService) Variants(ctx context.Context, assets ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error) {
	if f.variantsFn != nil {
		return f.variantsFn(ctx, assets...)
	}

	return nil, nil
}

func (f *fakeMediaService) Usage(ctx context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error) {
	return f.usageFn(ctx, filter)
}

func (f *fakeMediaService) PresignUpload(ctx context.Context, uploadedBy *uuid.UUID, filename, contentType string, sizeBytes int64, tags []string) (mediadomain.PresignResult, error) {
	return f.presignFn(ctx, uploadedBy, filename, contentType, sizeBytes, tags)
}

func (f *fakeMediaService) CompleteUpload(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	return f.completeFn(ctx, id)
}

func (f *fakeMediaService) List(ctx context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
	if f.listFn != nil {
		return f.listFn(ctx, filter)
	}

	return mediadomain.ListResult{}, nil
}

func (f *fakeMediaService) Get(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) GetReady(ctx context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
	if f.getReadyFn != nil {
		return f.getReadyFn(ctx, id)
	}

	return mediadomain.MediaAsset{}, mediaservice.ErrNotFound
}

func (f *fakeMediaService) Delete(ctx context.Context, id uuid.UUID) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}

	return nil
}

func (f *fakeMediaService) UpdateTags(ctx context.Context, id uuid.UUID, tags []string) (mediadomain.MediaAsset, error) {
	if f.tagsFn != nil {
		return f.tagsFn(ctx, id, tags)
	}

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
	body := `{"originalFilename":"photo.png","contentType":"image/png","sizeBytes":1024}`
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
	require.Equal(t, "https://storage.example/upload", data["uploadUrl"])
	require.Equal(t, mediaID.String(), data["mediaId"])
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
	c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodPost, "/api/v1/admin/media", bytes.NewBufferString(`{"originalFilename":"x","contentType":"text/plain","sizeBytes":1}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.ContextUserIDKey, uuid.New())

	adminPresignMediaHandler(svc)(c)

	require.Equal(t, nethttp.StatusBadRequest, w.Code)

	var envelope responses.Envelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.False(t, envelope.OK)
	require.Equal(t, "validation_error", envelope.Error.Code)
}

func TestMediaPresignRejectsBeforeCallingService(t *testing.T) {
	t.Parallel()

	svc := &fakeMediaService{presignFn: func(context.Context, *uuid.UUID, string, string, int64, []string) (mediadomain.PresignResult, error) {
		t.Fatal("presign should not be called")
		return mediadomain.PresignResult{}, nil
	}}

	tests := []struct {
		name   string
		user   *uuid.UUID
		body   string
		status int
		code   string
	}{
		{name: "anonymous", body: `{}`, status: nethttp.StatusUnauthorized, code: "unauthorized"},
		{name: "missing fields", user: &testUserID, body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w, body := runProfile(t, adminPresignMediaHandler(svc), profileRequest{
				method: nethttp.MethodPost, target: "/api/v1/admin/media", contentType: jsonContent, body: tc.body, user: tc.user,
			})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))
		})
	}
}

func TestMediaUsageReport(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userID := uuid.New()

	var got mediadomain.UsageFilter

	svc := &fakeMediaService{usageFn: func(_ context.Context, filter mediadomain.UsageFilter) (mediadomain.Usage, error) {
		got = filter

		return mediadomain.Usage{
			Total:        mediadomain.UsageRow{Count: 3, Bytes: 900},
			ByStatus:     []mediadomain.UsageRow{{Key: mediadomain.StatusReady, Count: 3, Bytes: 900}},
			TopUploaders: []mediadomain.UploaderUsage{{UserUUID: &userID, Username: "ana", Count: 2, Bytes: 600}, {Count: 1, Bytes: 300}},
		}, nil
	}}

	serve := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequestWithContext(t.Context(), nethttp.MethodGet, "/api/v1/admin/media/usage"+query, nil)
		adminMediaUsageHandler(svc)(c)

		return w
	}

	require.Equal(t, nethttp.StatusBadRequest, serve("?userId=nope").Code)

	w := serve("?userId=" + userID.String() + "&top=5")
	require.Equal(t, nethttp.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, 5, got.TopLimit)
	require.Equal(t, userID, *got.UploadedBy)

	var envelope struct {
		Data struct {
			Total        map[string]float64 `json:"total"`
			ByStatus     []map[string]any   `json:"byStatus"`
			TopUploaders []map[string]any   `json:"topUploaders"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.InDelta(t, 900, envelope.Data.Total["bytes"], 0)
	require.Equal(t, "ready", envelope.Data.ByStatus[0]["status"])
	require.Equal(t, "ana", envelope.Data.TopUploaders[0]["username"])
	require.Nil(t, envelope.Data.TopUploaders[1]["userId"])
}

func TestMediaUsageReportMapsServiceErrors(t *testing.T) {
	t.Parallel()

	svc := &fakeMediaService{usageFn: func(context.Context, mediadomain.UsageFilter) (mediadomain.Usage, error) {
		return mediadomain.Usage{}, fmt.Errorf("%w: top out of range", mediaservice.ErrValidation)
	}}

	w, body := runProfile(t, adminMediaUsageHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/?top=5"})
	require.Equal(t, nethttp.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "validation_error", errorCode(body))
	require.Empty(t, w.Header().Get("Cache-Control"))
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

func sampleAsset() mediadomain.MediaAsset {
	return mediadomain.MediaAsset{UUID: uuid.New(), ContentType: "image/png", Status: mediadomain.StatusReady, Tags: []string{"hero"}}
}

func TestMediaCompleteReturnsAssetWithVariants(t *testing.T) {
	t.Parallel()

	asset := sampleAsset()
	svc := &fakeMediaService{
		completeFn: func(_ context.Context, id uuid.UUID) (mediadomain.MediaAsset, error) {
			require.Equal(t, asset.UUID, id)
			return asset, nil
		},
		variantsFn: func(_ context.Context, assets ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error) {
			require.Len(t, assets, 1)
			return map[uuid.UUID]map[string]string{asset.UUID: {"thumb": "https://img/thumb"}}, nil
		},
	}

	w, body := runProfile(t, adminCompleteMediaHandler(svc), profileRequest{method: nethttp.MethodPost, target: "/complete", param: asset.UUID.String()})
	require.Equal(t, nethttp.StatusOK, w.Code, w.Body.String())
	require.Equal(t, asset.UUID.String(), dataOf(body)["id"])
	require.Equal(t, map[string]any{"thumb": "https://img/thumb"}, dataOf(body)["variants"])

	w, body = runProfile(t, adminCompleteMediaHandler(svc), profileRequest{method: nethttp.MethodPost, target: "/complete", param: "nope"})
	require.Equal(t, nethttp.StatusBadRequest, w.Code)
	require.Equal(t, "Invalid media id", errorMessage(body))
}

func TestAdminListMediaHandler(t *testing.T) {
	t.Parallel()

	asset := sampleAsset()
	tests := []struct {
		name        string
		query       string
		listErr     error
		variantsErr error
		status      int
		code        string
		wantFilter  mediadomain.ListFilter
	}{
		{
			name: "passes filters", query: "?page=2&perPage=5&q=cat&disk=s3&status=ready&unused=true", status: nethttp.StatusOK,
			wantFilter: mediadomain.ListFilter{
				PageRequest: pagination.PageRequest{Mode: pagination.ModeOffset, Forward: true, Page: 2, Limit: 5, Offset: 0, IncludeTotal: true},
				Query: "cat", Disk: "s3", Status: "ready", Unused: true,
			},
		},
		{
			name: "invalid paging falls back", query: "?page=-1&perPage=x&unused=true", status: nethttp.StatusOK,
			wantFilter: mediadomain.ListFilter{
				PageRequest: pagination.PageRequest{Mode: pagination.ModeOffset, Forward: true, Page: 1, Limit: 20, IncludeTotal: true},
				Unused: true,
			},
		},
		{
			name: "list failure", listErr: mediaservice.ErrValidation, status: nethttp.StatusBadRequest, code: "validation_error",
			wantFilter: mediadomain.ListFilter{
				PageRequest: pagination.PageRequest{Forward: true, Limit: 20, IncludeTotal: true},
			},
		},
		{
			name: "variants failure", variantsErr: mediaservice.ErrStorage, status: nethttp.StatusBadGateway, code: "storage_unavailable",
			wantFilter: mediadomain.ListFilter{
				PageRequest: pagination.PageRequest{Forward: true, Limit: 20, IncludeTotal: true},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got mediadomain.ListFilter

			svc := &fakeMediaService{
				listFn: func(_ context.Context, filter mediadomain.ListFilter) (mediadomain.ListResult, error) {
					got = filter
					total := int64(1)
					return mediadomain.ListResult{
						Items:         []mediadomain.MediaAsset{asset},
						Total:         &total,
						Limit:         filter.Limit,
						OffsetPage:    filter.Page,
						OffsetPerPage: filter.Limit,
					}, tc.listErr
				},
				variantsFn: func(context.Context, ...mediadomain.MediaAsset) (map[uuid.UUID]map[string]string, error) {
					return nil, tc.variantsErr
				},
			}

			w, body := runProfile(t, adminListMediaHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/api/v1/admin/media" + tc.query})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.wantFilter, got)

			if tc.status == nethttp.StatusOK {
				data, ok := body["data"].([]any)
				require.True(t, ok)
				require.Len(t, data, 1)
			}
		})
	}
}

func TestAdminDeleteMediaHandler(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	tests := []struct {
		name   string
		param  string
		err    error
		status int
		code   string
	}{
		{name: "invalid id", param: "nope", status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "deleted", param: id.String(), status: nethttp.StatusOK},
		{name: "not found", param: id.String(), err: mediaservice.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeMediaService{deleteFn: func(_ context.Context, got uuid.UUID) error {
				require.Equal(t, id, got)
				return tc.err
			}}
			w, body := runProfile(t, adminDeleteMediaHandler(svc), profileRequest{method: nethttp.MethodDelete, target: "/", param: tc.param})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))
			require.Equal(t, tc.status == nethttp.StatusOK, body["ok"] == true)
		})
	}
}

func TestAdminPatchMediaTagsHandler(t *testing.T) {
	t.Parallel()

	asset := sampleAsset()
	tests := []struct {
		name   string
		param  string
		body   string
		err    error
		status int
		code   string
	}{
		{name: "invalid id", param: "nope", body: `{"tags":[]}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "missing tags", param: asset.UUID.String(), body: `{}`, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "updated", param: asset.UUID.String(), body: `{"tags":["hero"]}`, status: nethttp.StatusOK},
		{name: "upload expired", param: asset.UUID.String(), body: `{"tags":["hero"]}`, err: mediaservice.ErrUploadExpired, status: nethttp.StatusConflict, code: "media.upload_expired"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeMediaService{tagsFn: func(_ context.Context, id uuid.UUID, tags []string) (mediadomain.MediaAsset, error) {
				require.Equal(t, asset.UUID, id)
				require.Equal(t, []string{"hero"}, tags)

				return asset, tc.err
			}}
			w, body := runProfile(t, adminPatchMediaTagsHandler(svc), profileRequest{
				method: nethttp.MethodPatch, target: "/tags", contentType: jsonContent, body: tc.body, param: tc.param,
			})
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, []any{"hero"}, dataOf(body)["tags"])
			}
		})
	}
}

func TestPublicGetMediaHandler(t *testing.T) {
	t.Parallel()

	asset := sampleAsset()
	tests := []struct {
		name   string
		param  string
		err    error
		status int
		code   string
	}{
		{name: "invalid id", param: "nope", status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "ready asset", param: asset.UUID.String(), status: nethttp.StatusOK},
		{name: "not ready", param: asset.UUID.String(), err: mediaservice.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := &fakeMediaService{getReadyFn: func(context.Context, uuid.UUID) (mediadomain.MediaAsset, error) {
				return asset, tc.err
			}}
			w, body := runProfile(t, publicGetMediaHandler(svc), profileRequest{method: nethttp.MethodGet, target: "/", param: tc.param})
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, errorCode(body))

			if tc.status == nethttp.StatusOK {
				require.Equal(t, asset.UUID.String(), dataOf(body)["id"])
				require.NotContains(t, dataOf(body), "variants")
			}
		})
	}
}

func TestRenderMediaWithoutService(t *testing.T) {
	t.Parallel()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	asset := sampleAsset()

	out, ok := renderMedia(c, nil, asset)
	require.True(t, ok)
	require.Len(t, out, 1)
	require.Equal(t, asset.UUID.String(), out[0]["id"])
	require.NotContains(t, out[0], "variants")
}

func TestMapMediaError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		status   int
		code     string
		recorded bool
	}{
		{name: "nil", err: nil},
		{name: "validation", err: mediaservice.ErrValidation, status: nethttp.StatusBadRequest, code: "validation_error"},
		{name: "not found", err: mediaservice.ErrNotFound, status: nethttp.StatusNotFound, code: "not_found"},
		{name: "incomplete", err: mediaservice.ErrUploadIncomplete, status: nethttp.StatusConflict, code: "media.upload_incomplete"},
		{name: "expired", err: mediaservice.ErrUploadExpired, status: nethttp.StatusConflict, code: "media.upload_expired"},
		{name: "storage", err: mediaservice.ErrStorage, status: nethttp.StatusBadGateway, code: "storage_unavailable", recorded: true},
		{name: "unknown", err: context.DeadlineExceeded, status: nethttp.StatusInternalServerError, code: "internal_error", recorded: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			require.Equal(t, tc.err != nil, mapMediaError(c, tc.err))
			require.Equal(t, tc.recorded, len(c.Errors) > 0)

			if tc.err == nil {
				require.Zero(t, w.Body.Len())
				return
			}

			var envelope responses.Envelope
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.code, envelope.Error.Code)
		})
	}
}

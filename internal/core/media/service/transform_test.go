package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

type fakeTransformer struct {
	got []mediadomain.Transform
	err error
}

func (f *fakeTransformer) URL(asset mediadomain.MediaAsset, t mediadomain.Transform) (string, error) {
	f.got = append(f.got, t)
	if f.err != nil {
		return "", f.err
	}

	return "https://img.example.com/" + asset.StorageKey, nil
}

func seedAsset(repo *fakeRepo, contentType, status string) uuid.UUID {
	id := uuid.New()
	repo.assets[id] = mediadomain.MediaAsset{UUID: id, StorageKey: "media/" + id.String(), ContentType: contentType, Status: status}

	return id
}

func TestTransformURLValidatesAndDelegates(t *testing.T) {
	t.Parallel()

	svc, repo, _ := newTestService()

	ready := seedAsset(repo, "image/png", mediadomain.StatusReady)
	pending := seedAsset(repo, "image/png", mediadomain.StatusPending)
	svg := seedAsset(repo, "image/svg+xml", mediadomain.StatusReady)

	_, err := svc.TransformURL(context.Background(), ready, mediadomain.Transform{Width: 256})
	if !errors.Is(err, ErrTransformDisabled) {
		t.Fatalf("expected ErrTransformDisabled without a transformer, got %v", err)
	}

	transformer := &fakeTransformer{}
	svc.WithTransforms(transformer, []int{128, 256})

	url, err := svc.TransformURL(context.Background(), ready, mediadomain.Transform{Width: 256, Format: mediadomain.FormatWebP})
	if err != nil || url != "https://img.example.com/media/"+ready.String() {
		t.Fatalf("unexpected url %q, err %v", url, err)
	}

	for name, tc := range map[string]struct {
		id   uuid.UUID
		tr   mediadomain.Transform
		want error
	}{
		"width not listed": {ready, mediadomain.Transform{Width: 300}, ErrValidation},
		"bad format":       {ready, mediadomain.Transform{Width: 128, Format: "tiff"}, ErrValidation},
		"not ready":        {pending, mediadomain.Transform{Width: 128}, ErrNotFound},
		"missing":          {uuid.New(), mediadomain.Transform{Width: 128}, ErrNotFound},
		"svg":              {svg, mediadomain.Transform{Width: 128}, ErrValidation},
	} {
		if _, err := svc.TransformURL(context.Background(), tc.id, tc.tr); !errors.Is(err, tc.want) {
			t.Fatalf("%s: expected %v, got %v", name, tc.want, err)
		}
	}

	if len(transformer.got) != 1 {
		t.Fatalf("transformer called for rejected requests: %v", transformer.got)
	}
}

func TestTransformURLFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("signer down")

	tests := []struct {
		name        string
		policy      fakePolicy
		transformer *fakeTransformer
	}{
		{name: "policy read fails", policy: fakePolicy{err: failure}, transformer: &fakeTransformer{}},
		{name: "signing fails", transformer: &fakeTransformer{err: failure}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newTestService()
			ready := seedAsset(repo, "image/png", mediadomain.StatusReady)
			svc.WithTransforms(tc.transformer, []int{256}).WithPolicy(tc.policy)

			_, err := svc.TransformURL(t.Context(), ready, mediadomain.Transform{Width: 256})

			require.ErrorIs(t, err, failure)
		})
	}
}

func TestVariantsFailuresAndSkips(t *testing.T) {
	t.Parallel()

	failure := errors.New("signer down")
	variants := mediadomain.TransformPolicy{Variants: []mediadomain.Variant{{Name: "thumb", Width: 100}}}

	tests := []struct {
		name        string
		policy      fakePolicy
		transformer *fakeTransformer
		status      string
		deleted     bool
		want        error
	}{
		{name: "policy read fails", policy: fakePolicy{err: failure}, transformer: &fakeTransformer{}, status: mediadomain.StatusReady, want: failure},
		{name: "signing fails", policy: fakePolicy{policy: variants}, transformer: &fakeTransformer{err: failure}, status: mediadomain.StatusReady, want: failure},
		{name: "pending asset gets no variants", policy: fakePolicy{policy: variants}, transformer: &fakeTransformer{}, status: mediadomain.StatusPending},
		{name: "deleted asset gets no variants", policy: fakePolicy{policy: variants}, transformer: &fakeTransformer{}, status: mediadomain.StatusReady, deleted: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc, repo, _ := newTestService()
			id := seedAsset(repo, "image/png", tc.status)
			asset := repo.assets[id]

			if tc.deleted {
				deleted := baseTime()
				asset.DeletedAt = &deleted
			}

			svc.WithTransforms(tc.transformer, nil).WithPolicy(tc.policy)

			got, err := svc.Variants(t.Context(), asset)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, got)

				return
			}

			require.NoError(t, err)
			require.Equal(t, map[uuid.UUID]map[string]string{id: {}}, got)
			require.Empty(t, tc.transformer.got)
		})
	}
}

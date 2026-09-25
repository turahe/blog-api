package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	mediadomain "github.com/turahe/blog-api/internal/core/media/domain"
)

type fakeTransformer struct {
	got []mediadomain.Transform
}

func (f *fakeTransformer) URL(asset mediadomain.MediaAsset, t mediadomain.Transform) (string, error) {
	f.got = append(f.got, t)

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

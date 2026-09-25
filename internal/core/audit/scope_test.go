package audit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/audit"
	"github.com/turahe/blog-api/internal/core/audit/domain"
)

func TestHelpersWithoutScopeAreNoOps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	require.NotPanics(t, func() {
		audit.SetActor(ctx, uuid.New())
		audit.SetResource(ctx, "post", uuid.New())
		audit.AddChange(ctx, "status", "draft", "published")
		audit.AddMetadata(ctx, "k", "v")
	})
}

func TestScopeApplyOverridesInferredFields(t *testing.T) {
	t.Parallel()

	ctx, scope := audit.WithScope(context.Background())
	actor, post := uuid.New(), uuid.New()

	audit.SetActor(ctx, actor)
	audit.SetResource(ctx, "post", post)
	audit.AddChange(ctx, "status", "draft", "published")
	audit.AddMetadata(ctx, "oauth_linked", "github")

	inferred := uuid.New()
	entry := domain.Entry{ResourceType: "user", ResourceID: &inferred, Metadata: map[string]any{"keep": true}}
	scope.Apply(&entry)

	require.Equal(t, &actor, entry.ActorID)
	require.Equal(t, &actor, scope.Actor())
	require.Equal(t, "post", entry.ResourceType)
	require.Equal(t, &post, entry.ResourceID)
	require.Equal(t, map[string]domain.Change{"status": {From: "draft", To: "published"}}, entry.Changes)
	require.Equal(t, map[string]any{"keep": true, "oauth_linked": "github"}, entry.Metadata)
}

func TestEmptyScopeKeepsEntry(t *testing.T) {
	t.Parallel()

	_, scope := audit.WithScope(context.Background())
	actor := uuid.New()
	entry := domain.Entry{ActorID: &actor, ResourceType: "user"}

	scope.Apply(&entry)

	require.Equal(t, &actor, entry.ActorID)
	require.Equal(t, "user", entry.ResourceType)
	require.Nil(t, entry.Changes)
	require.Nil(t, entry.Metadata)
	require.Nil(t, scope.Actor())
}

package readcache_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/core/readcache"
)

func TestKeyNormalisesQueryParts(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	var none *uuid.UUID

	require.Equal(t, "list", readcache.Key("list"))
	require.Equal(t,
		"list:page=2:per_page=20:category=11111111-1111-1111-1111-111111111111:tag=-",
		readcache.Key("list", "page", 2, "per_page", 20, "category", &id, "tag", none))
	require.Equal(t, "get:slug=-", readcache.Key("get", "slug", ""))
}

package pagination_test

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

func newCtx(t *testing.T, query url.Values) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	raw := ""
	if len(query) > 0 {
		raw = "?" + query.Encode()
	}
	req := httptest.NewRequest("GET", "/api/v1/posts"+raw, nil)
	ctx.Request = req
	return ctx
}

func TestParseRequestCursorAfter(t *testing.T) {
	t.Parallel()
	q := url.Values{}
	q.Set("after", "XYZABC")
	q.Set("limit", "5")
	q.Set("includeTotal", "false")
	pr, err := pagination.ParseRequest(newCtx(t, q), cfgPost, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, pagination.ModeCursor, pr.Mode)
	require.True(t, pr.Forward)
	require.Equal(t, "XYZABC", pr.Cursor)
	require.Equal(t, 5, pr.Limit)
	require.False(t, pr.IncludeTotal)
	require.Equal(t, 0, pr.Offset)
}

func TestParseRequestCursorBefore(t *testing.T) {
	t.Parallel()
	q := url.Values{}
	q.Set("before", "BEF")
	pr, err := pagination.ParseRequest(newCtx(t, q), cfgPost, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, pagination.ModeCursor, pr.Mode)
	require.False(t, pr.Forward)
	require.Equal(t, "BEF", pr.Cursor)
}

func TestParseRequestAfterWinsOverPage(t *testing.T) {
	t.Parallel()
	q := url.Values{}
	q.Set("after", "X")
	q.Set("page", "99")
	pr, err := pagination.ParseRequest(newCtx(t, q), cfgPost, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, pagination.ModeCursor, pr.Mode)
	require.Equal(t, "X", pr.Cursor)
}

func TestParseRequestLegacyOffset(t *testing.T) {
	t.Parallel()
	q := url.Values{}
	q.Set("page", "3")
	q.Set("perPage", "7")
	pr, err := pagination.ParseRequest(newCtx(t, q), cfgPost, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, pagination.ModeOffset, pr.Mode)
	require.Equal(t, 3, pr.Page)
	require.Equal(t, 7, pr.Limit)
	require.Equal(t, 0, pr.Offset)
	require.True(t, pr.IncludeTotal)
}

func TestParseRequestDefaults(t *testing.T) {
	t.Parallel()
	pr, err := pagination.ParseRequest(newCtx(t, nil), cfgPost, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, pagination.ModeCursor, pr.Mode)
	require.True(t, pr.Forward)
	require.Equal(t, 20, pr.Limit)
	require.True(t, pr.IncludeTotal)
}

func TestParseRequestLimitCaps(t *testing.T) {
	t.Parallel()
	cfg := cfgPost
	// Default max per cfgPost: 100.
	cases := []struct {
		in   string
		want int
	}{
		{"9999", 100},
		{"0", 20},
		{"-5", 20},
		{"", 20},
		{"not-a-number", 20},
		{"50", 50},
	}
	for _, tc := range cases {
		tc := tc
		t.Run("limit="+tc.in, func(t *testing.T) {
			t.Parallel()
			q := url.Values{}
			if tc.in != "" {
				q.Set("limit", tc.in)
			}
			pr, err := pagination.ParseRequest(newCtx(t, q), cfg, pagination.WithSigner(signer))
			require.NoError(t, err)
			require.Equal(t, tc.want, pr.Limit)
		})
	}
}

func TestParseRequestPerPageAlias(t *testing.T) {
	t.Parallel()
	cfg := cfgPost
	q := url.Values{}
	q.Set("perPage", "12")
	pr, err := pagination.ParseRequest(newCtx(t, q), cfg, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, 12, pr.Limit)
	// limit wins over perPage when both set.
	q.Set("limit", "14")
	pr, err = pagination.ParseRequest(newCtx(t, q), cfg, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, 14, pr.Limit)
}

func TestParseRequestIncludeTotalBooleans(t *testing.T) {
	t.Parallel()
	cases := []struct {
		v    string
		want bool
	}{
		{"", true}, {"true", true}, {"1", true}, {"yes", true},
		{"false", false}, {"0", false}, {"no", false}, {"off", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.v, func(t *testing.T) {
			t.Parallel()
			q := url.Values{}
			if tc.v != "" {
				q.Set("includeTotal", tc.v)
			}
			pr, err := pagination.ParseRequest(newCtx(t, q), cfgPost, pagination.WithSigner(signer))
			require.NoError(t, err)
			require.Equal(t, tc.want, pr.IncludeTotal)
		})
	}
}

func TestParseRequestOffsetModeOnlyConfigRejectsCursors(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{Kind: "search", Sort: []pagination.SortField{{Name: "id", Type: pagination.TypeInt64}}, OffsetModeOnly: true}
	q := url.Values{}
	q.Set("after", "BAD")
	_, err := pagination.ParseRequest(newCtx(t, q), cfg, pagination.WithSigner(signer))
	require.ErrorIs(t, err, pagination.ErrCursorUnsupported)
	require.Equal(t, pagination.CodeUnsupported, pagination.ErrorCode(err))
}

func TestParseLegacyClamps(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{Kind: "x", Sort: []pagination.SortField{{Name: "id", Type: pagination.TypeInt64}}}
	pr := pagination.ParseLegacy(cfg, 0, -4)
	require.Equal(t, pagination.ModeOffset, pr.Mode)
	require.Equal(t, 1, pr.Page)
	require.Equal(t, 20, pr.Limit)
	require.Equal(t, 0, pr.Offset)
	require.True(t, pr.IncludeTotal)
	pr = pagination.ParseLegacy(cfg, 2, 999999)
	require.Equal(t, 100, pr.Limit)
	require.Equal(t, 100, pr.Offset)
}

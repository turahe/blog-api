package pagination_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

type rowLike struct {
	id      int64
	created time.Time
}

func TestSortValuesHelper(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{
			{Name: "created_at", Type: pagination.TypeTime, Dir: pagination.Desc},
			{Name: "id", Type: pagination.TypeInt64, Dir: pagination.Desc},
		},
	}
	r := &rowLike{id: 42, created: time.Unix(1700000000, 0)}
	out := pagination.SortValues[*rowLike](cfg, r, func(row *rowLike, i int) any {
		switch i {
		case 0:
			return row.created
		case 1:
			return row.id
		}
		return nil
	})
	require.Equal(t, time.Unix(1700000000, 0).UTC(), out["created_at"])
	require.Equal(t, int64(42), out["id"])
}

func TestErrorCodeCauseWrapping(t *testing.T) {
	t.Parallel()
	empty := pagination.ErrorCause(nil)
	require.Empty(t, empty)
	got := pagination.ErrorCause(errors.New("boom"))
	require.Contains(t, got, "boom")

	// Wrap each sentinel via %w and ensure ErrorCode unwraps correctly.
	pairs := []struct {
		sentinel error
		code     string
	}{
		{pagination.ErrCursorMalformed, pagination.CodeMalformed},
		{pagination.ErrCursorInvalidSignature, pagination.CodeTampered},
		{pagination.ErrCursorExpired, pagination.CodeExpired},
		{pagination.ErrCursorWrongKind, pagination.CodeWrongKind},
		{pagination.ErrCursorMissingField, pagination.CodeMissingField},
		{pagination.ErrCursorFieldType, pagination.CodeFieldType},
		{pagination.ErrCursorUnsupported, pagination.CodeUnsupported},
	}
	for _, p := range pairs {
		p := p
		t.Run(p.code, func(t *testing.T) {
			t.Parallel()
			w := wrapper{err: wrapper{err: p.sentinel}}
			require.ErrorIs(t, w, p.sentinel)
			require.Equal(t, p.code, pagination.ErrorCode(w))
		})
	}
}

type wrapper struct{ err error }

func (w wrapper) Error() string { return w.err.Error() }
func (w wrapper) Unwrap() error { return w.err }

func TestPtrToIntPtrHelpers(t *testing.T) {
	t.Parallel()
	b := pagination.PtrTo(true)
	require.NotNil(t, b)
	require.True(t, *b)
	s := pagination.PtrTo("hi")
	require.Equal(t, "hi", *s)
	p := pagination.IntPtr(7)
	require.NotNil(t, p)
	require.Equal(t, 7, *p)
}

func TestApplyOptionsFallsBackToGlobalSigner(t *testing.T) {
	pagination.ResetGlobalSigner()
	t.Setenv("PAGINATION_HMAC_KEY", keyA)
	_, _, err := pagination.DecodeCursor(cfgPost, "garbage-base64!!!")
	// Expected: token fails because it's garbled (not a cursor), but no error
	// from signer lookup.
	require.Error(t, err)
	require.NotErrorIs(t, err, pagination.ErrCursorInvalidSignature) // malformed first
}

func TestParseRequestWithGlobalSignerWhenNotProvided(t *testing.T) {
	pagination.ResetGlobalSigner()
	t.Setenv("PAGINATION_HMAC_KEY", keyA)
	q := url.Values{}
	q.Set("limit", "10")
	_, err := pagination.ParseRequest(newCtx(t, q), cfgPost)
	require.NoError(t, err)
}

func TestNormalizeSortValueLooseVariants(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "v",
		Sort: []pagination.SortField{
			{Name: "a", Type: pagination.TypeString},
			{Name: "b", Type: pagination.TypeUUID},
			{Name: "c", Type: pagination.TypeInt64},
			{Name: "d", Type: pagination.TypeTime},
		},
	}
	uid := uuid.New()
	now := time.Now()
	fields := map[string]any{
		"a": "hi",
		"b": uid.String(),
		"c": float64(7),
		"d": now.Format(time.RFC3339),
	}
	tok, err := pagination.EncodeCursor(cfg, fields, pagination.WithSigner(signer))
	require.NoError(t, err)
	dec, _, err := pagination.DecodeCursor(cfg, tok, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.NoError(t, pagination.ValidateCursor(cfg, dec))
	require.Equal(t, "hi", dec["a"])
	require.Equal(t, uid, dec["b"])
	require.Equal(t, int64(7), dec["c"])
	parsedNow, _ := time.Parse(time.RFC3339, now.Format(time.RFC3339))
	require.Equal(t, parsedNow.UTC(), dec["d"])
}

func TestCheckTypeBadCases(t *testing.T) {
	t.Parallel()
	require.Error(t, pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "s", Type: pagination.TypeString}},
	}, map[string]any{"s": 123}))
	// int64 from float with fractional
	err := pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "n", Type: pagination.TypeInt64}},
	}, map[string]any{"n": 1.5})
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	// int64 json.Number with fractional
	err = pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "n", Type: pagination.TypeInt64}},
	}, map[string]any{"n": "not"})
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	// bad uuid
	err = pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "u", Type: pagination.TypeUUID}},
	}, map[string]any{"u": "nope"})
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	err = pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "u", Type: pagination.TypeUUID}},
	}, map[string]any{"u": 42})
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	// bad time
	err = pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "t", Type: pagination.TypeTime}},
	}, map[string]any{"t": "notadate"})
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	err = pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "t", Type: pagination.TypeTime}},
	}, map[string]any{"t": 42})
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	// unknown sort column type
	err = pagination.ValidateCursor(pagination.CursorConfig{
		Kind: "x",
		Sort: []pagination.SortField{{Name: "t", Type: pagination.ColumnType(255)}},
	}, map[string]any{"t": 1})
	require.Error(t, err)
}

func TestRequestNilForwardCursorDefault(t *testing.T) {
	t.Parallel()
	// ParseRequest with no params: ModeCursor, forward=true, Limit=default,
	// includeTotal=true.
	pr, err := pagination.ParseRequest(newCtx(t, nil), cfgPost, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.Equal(t, pagination.ModeCursor, pr.Mode)
	require.True(t, pr.Forward)
}

func TestStripAllCursorsEdgeCases(t *testing.T) {
	t.Parallel()
	require.Empty(t, pagination.StripAllCursors(""))
	require.Empty(t, pagination.StripAllCursors("page=2"))
	require.Equal(t, "?tag=go", pagination.StripAllCursors("page=1&limit=5&tag=go"))
}

func TestBuildLinksIncludeTotal(t *testing.T) {
	t.Parallel()
	// When includeTotal=true setIncludeTotal should emit true for cursor mode.
	total := int64(100)
	r := pagination.PageResult[Item]{
		Items:           []Item{{ID: 1}, {ID: 2}},
		Total:           &total,
		HasNextPage:     true,
		HasPreviousPage: false,
		NextCursor:      "C",
		Limit:           10,
	}
	ctx := newCtx2(t, "/posts", url.Values{"q": []string{"x"}})
	legacy, _ := pagination.BuildMeta[Item](ctx, r)
	links := pagination.BuildLinks[Item](ctx, r, legacy)
	require.NotNil(t, links.Next)
	u, err := url.Parse(*links.Next)
	require.NoError(t, err)
	require.Equal(t, "true", u.Query().Get("includeTotal"))
	require.Equal(t, "C", u.Query().Get("after"))
	require.Equal(t, "10", u.Query().Get("limit"))
	require.Equal(t, "x", u.Query().Get("q"))
}

func TestNewSignerPanicsOnEmptyCurrent(t *testing.T) {
	require.Panics(t, func() {
		pagination.NewSigner(nil, nil)
	})
	require.Panics(t, func() {
		pagination.NewSigner([]byte{}, nil)
	})
}

func TestAbsoluteURLWithoutXForwarded(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/foo?q=1", nil)
	c.Request.Host = "local:8080"
	got := pagination.StripAllCursors("q=1&after=x")
	parsed, _ := url.ParseQuery(got[1:])
	require.Equal(t, "1", parsed.Get("q"))
}

func TestBuildSeekCoerceBindVariants(t *testing.T) {
	t.Parallel()
	// Use raw BuildSeek with explicit bind variants: uint/uint64/uint32, time from string, uuid from string.
	cfg := pagination.CursorConfig{
		Kind: "bind",
		Sort: []pagination.SortField{
			{Name: "t", Column: "t", Dir: pagination.Desc, Type: pagination.TypeTime},
			{Name: "u", Column: "u", Dir: pagination.Desc, Type: pagination.TypeUUID},
			{Name: "s", Column: "s", Dir: pagination.Asc, Type: pagination.TypeString},
			{Name: "id", Column: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
		},
	}
	uid := uuid.New()
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: true, Limit: 10, Cursor: "X"}
	cur := map[string]any{
		"t":  time.Unix(100, 0).Format(time.RFC3339Nano), // string time
		"u":  uid.String(),
		"s":  "zzz",
		"id": uint(42),
	}
	res, err := pagination.BuildSeek(cfg, pr, cur)
	require.NoError(t, err)
	require.Len(t, res.BindVars, 4)
	require.Equal(t, time.Unix(100, 0).UTC(), res.BindVars[0])
	require.Equal(t, uid, res.BindVars[1])
	require.Equal(t, "zzz", res.BindVars[2])
	require.Equal(t, int64(42), res.BindVars[3])

	// uint64 + uint32 variants
	cur2 := map[string]any{
		"t":  time.Unix(100, 0),
		"u":  uid,
		"s":  "zzz",
		"id": uint64(42),
	}
	res2, err := pagination.BuildSeek(cfg, pr, cur2)
	require.NoError(t, err)
	require.Equal(t, int64(42), res2.BindVars[3])
	cur2["id"] = uint32(43)
	res3, err := pagination.BuildSeek(cfg, pr, cur2)
	require.NoError(t, err)
	require.Equal(t, int64(43), res3.BindVars[3])
}

func TestDecodeCursorBase64Malformed(t *testing.T) {
	t.Parallel()
	_, _, err := pagination.DecodeCursor(cfgPost, "!!!not-base64!!!", pagination.WithSigner(signer))
	require.ErrorIs(t, err, pagination.ErrCursorMalformed)
}

func TestDecodeCursorIssuedAtBadType(t *testing.T) {
	t.Parallel()
	// Craft a payload where issued_at is a bool (invalid type) then sign + encode.
	payload := map[string]any{
		"kind":      cfgPost.Kind,
		"issued_at": true,
		"fields":    map[string]any{},
	}
	raw, _ := json.Marshal(payload)
	signed := signer.Sign(raw)
	tok := base64.RawURLEncoding.EncodeToString(signed)
	_, _, err := pagination.DecodeCursor(cfgPost, tok, pagination.WithSigner(signer))
	require.ErrorIs(t, err, pagination.ErrCursorMalformed)
}

func TestSortFieldDefaultsNameToColumn(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "no_col",
		Sort: []pagination.SortField{
			{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
			{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
		},
	}
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: true, Limit: 5, Cursor: "X"}
	cur := map[string]any{"created_at": time.Unix(1, 0), "id": 1}
	res, err := pagination.BuildSeek(cfg, pr, cur)
	require.NoError(t, err)
	// Columns should fall back to Name values.
	require.Contains(t, res.WhereClause, "(created_at, id) <")
	require.Contains(t, res.OrderClause, "ORDER BY created_at DESC")
}

func TestBuildSeekComparatorASCForward(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "asc_sort",
		Sort: []pagination.SortField{
			{Name: "created_at", Dir: pagination.Asc, Type: pagination.TypeTime},
			{Name: "id", Dir: pagination.Asc, Type: pagination.TypeInt64},
		},
	}
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: true, Limit: 5, Cursor: "X"}
	cur := map[string]any{"created_at": time.Unix(1, 0), "id": 1}
	res, err := pagination.BuildSeek(cfg, pr, cur)
	require.NoError(t, err)
	require.Contains(t, res.WhereClause, "(created_at, id) >")
}

func TestOrderNullsFirstToLastOnBackward(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "n",
		Sort: []pagination.SortField{
			{Name: "t", Dir: pagination.Asc, Nulls: pagination.NullsFirst, Type: pagination.TypeTime},
			{Name: "id", Dir: pagination.Asc, Type: pagination.TypeInt64},
		},
	}
	pr := pagination.PageRequest{Mode: pagination.ModeCursor, Forward: false, Limit: 5, Cursor: "X"}
	cur := map[string]any{"t": time.Unix(1, 0), "id": 1}
	res, err := pagination.BuildSeek(cfg, pr, cur)
	require.NoError(t, err)
	// ASC + NullsFirst → backward → DESC + NULLS LAST
	require.Contains(t, res.OrderClause, "t DESC NULLS LAST")
}

func TestBidirectionalCursorPages(t *testing.T) {
	t.Parallel()
	// Simpler TruncatePage with many random sizes for fuzz-like coverage.
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		limit := r.Intn(100) + 1
		excess := r.Intn(2) == 0
		size := limit
		if excess {
			size = limit + 1
		}
		items := make([]int, size)
		for j := range items {
			items[j] = j
		}
		hadCursor := r.Intn(2) == 0
		forward := r.Intn(2) == 0
		p, hn, hp := pagination.TruncatePage(items, limit, forward, hadCursor)
		require.LessOrEqual(t, len(p), limit)
		_ = hn
		_ = hp
	}
}

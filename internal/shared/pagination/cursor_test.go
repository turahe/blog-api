package pagination_test

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

var (
	cfgPost = pagination.CursorConfig{
		Kind: "posts_public",
		Sort: []pagination.SortField{
			{Name: "published_at", Column: "p.published_at", Dir: pagination.Desc, Nulls: pagination.NullsLast, Type: pagination.TypeTime},
			{Name: "created_at", Column: "p.created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
			{Name: "id", Column: "p.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
		},
		TTL:            time.Hour,
		MaxPerPage:     100,
		DefaultPerPage: 20,
	}
	cfgComment = pagination.CursorConfig{
		Kind: "comments_newest",
		Sort: []pagination.SortField{
			{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
			{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
		},
		TTL: pagination.DefaultTTL,
	}
	signer = pagination.NewSigner([]byte(keyA), nil)
)

func TestEncodeDecodeCursorRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	fields := map[string]any{
		"published_at": now.Add(-3 * time.Hour),
		"created_at":   now.Add(-2 * time.Hour),
		"id":           int64(12345),
	}
	tok, err := pagination.EncodeCursor(cfgPost, fields, pagination.WithSigner(signer), pagination.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	require.NotEmpty(t, tok)
	// Token must be URL-safe base64 (no padding, no '+', no '/').
	_, err = base64.RawURLEncoding.DecodeString(tok)
	require.NoError(t, err, "token must be raw URL base64")

	decoded, issuedAt, err := pagination.DecodeCursor(cfgPost, tok, pagination.WithSigner(signer), pagination.WithNow(func() time.Time { return now.Add(10 * time.Minute) }))
	require.NoError(t, err)
	require.WithinDuration(t, now, issuedAt, time.Second)
	require.Equal(t, now.Add(-3*time.Hour).UTC(), decoded["published_at"])
	require.Equal(t, now.Add(-2*time.Hour).UTC(), decoded["created_at"])
	require.Equal(t, int64(12345), decoded["id"])
	require.NoError(t, pagination.ValidateCursor(cfgPost, decoded))
}

func TestCursorExpiry(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	fields := map[string]any{"published_at": now, "created_at": now, "id": int64(1)}
	tok, err := pagination.EncodeCursor(cfgPost, fields, pagination.WithSigner(signer), pagination.WithNow(func() time.Time { return now }))
	require.NoError(t, err)
	// TTL for cfgPost is 1h. 2h later = expired.
	_, _, err = pagination.DecodeCursor(cfgPost, tok, pagination.WithSigner(signer), pagination.WithNow(func() time.Time { return now.Add(2 * time.Hour) }))
	require.ErrorIs(t, err, pagination.ErrCursorExpired)
	require.Equal(t, pagination.CodeExpired, pagination.ErrorCode(err))
}

func TestCursorWrongKind(t *testing.T) {
	t.Parallel()
	now := time.Now()
	fields := map[string]any{"published_at": now, "created_at": now, "id": int64(1)}
	tok, err := pagination.EncodeCursor(cfgPost, fields, pagination.WithSigner(signer))
	require.NoError(t, err)
	_, _, err = pagination.DecodeCursor(cfgComment, tok, pagination.WithSigner(signer))
	require.ErrorIs(t, err, pagination.ErrCursorWrongKind)
	require.Equal(t, pagination.CodeWrongKind, pagination.ErrorCode(err))
}

func TestCursorTamperedByteFlipping(t *testing.T) {
	t.Parallel()
	now := time.Now()
	fields := map[string]any{"published_at": now, "created_at": now, "id": int64(77)}
	tok, err := pagination.EncodeCursor(cfgPost, fields, pagination.WithSigner(signer))
	require.NoError(t, err)

	for i := 0; i < 100; i++ {
		mutated := mutate(t, tok, i)
		_, _, err := pagination.DecodeCursor(cfgPost, mutated, pagination.WithSigner(signer))
		code := pagination.ErrorCode(err)
		require.Contains(t, []string{pagination.CodeTampered, pagination.CodeMalformed}, code,
			"iteration %d got code %s err=%v token=%q", i, code, err, mutated)
	}
}

// mutate returns a byte mutation of base64url token tok: flip one random bit,
// or truncate by one char for half the iterations.
func mutate(t *testing.T, tok string, seed int) string {
	t.Helper()
	b := []byte(tok)
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(b)*2)))
	idx := int(n.Int64()) % len(b)
	switch {
	case seed%5 == 0:
		return tok[:len(tok)-1]
	case seed%5 == 1 && len(tok) > 4:
		return tok[1:]
	case seed%5 == 2:
		// append random printable char
		c, _ := rand.Int(rand.Reader, big.NewInt(26))
		return tok + string(rune('a'+int(c.Int64())))
	default:
		b[idx] ^= 0x01
		return string(b)
	}
}

func TestCursorMissingField(t *testing.T) {
	t.Parallel()
	now := time.Now()
	// Missing created_at.
	fields := map[string]any{"published_at": now, "id": int64(9)}
	_, err := pagination.EncodeCursor(cfgPost, fields, pagination.WithSigner(signer))
	require.ErrorIs(t, err, pagination.ErrCursorMissingField)
	require.Equal(t, pagination.CodeMissingField, pagination.ErrorCode(err))
}

func TestCursorValidateBadType(t *testing.T) {
	t.Parallel()
	now := time.Now()
	fields := map[string]any{"published_at": now, "created_at": now, "id": "not-an-int"}
	// Encode accepts string-for-int because Encode normalizes — here id comes
	// back as string "not-an-int" since normalizer for TypeInt64 does not coerce strings.
	// Call ValidateCursor explicitly via a manual map:
	err := pagination.ValidateCursor(cfgPost, fields)
	require.ErrorIs(t, err, pagination.ErrCursorFieldType)
	require.Equal(t, pagination.CodeFieldType, pagination.ErrorCode(err))
}

func TestCursorUUIDTypeCoercion(t *testing.T) {
	t.Parallel()
	cfg := pagination.CursorConfig{
		Kind: "by_uuid",
		Sort: []pagination.SortField{{Name: "u", Type: pagination.TypeUUID, Dir: pagination.Asc}},
		TTL:  time.Hour,
	}
	u := uuid.New()
	tok, err := pagination.EncodeCursor(cfg, map[string]any{"u": u.String()}, pagination.WithSigner(signer))
	require.NoError(t, err)
	decoded, _, err := pagination.DecodeCursor(cfg, tok, pagination.WithSigner(signer))
	require.NoError(t, err)
	require.NoError(t, pagination.ValidateCursor(cfg, decoded))
	got, ok := decoded["u"].(uuid.UUID)
	require.True(t, ok)
	require.Equal(t, u, got)
}

func TestErrorCodeFallback(t *testing.T) {
	require.Equal(t, pagination.CodeMalformed, pagination.ErrorCode(nil))
}

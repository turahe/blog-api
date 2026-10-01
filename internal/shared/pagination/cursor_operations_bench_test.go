package pagination

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// BenchmarkEncodeCursor_ThreeFields benchmarks EncodeCursor with 3 fields (typical: timestamp, timestamp, int64).
// Measures: HMAC-SHA256, JSON marshal, base64 encoding overhead.
func BenchmarkEncodeCursor_ThreeFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_posts",
		Sort: []SortField{
			{Name: "published_at", Type: TypeTime},
			{Name: "created_at", Type: TypeTime},
			{Name: "id", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"published_at": now.Add(-1 * time.Hour),
		"created_at":   now.Add(-2 * time.Hour),
		"id":           int64(99999),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkEncodeCursor_FiveFields benchmarks EncodeCursor with 5 fields.
// Measures: payload size impact on encoding performance.
func BenchmarkEncodeCursor_FiveFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_complex",
		Sort: []SortField{
			{Name: "field1", Type: TypeTime},
			{Name: "field2", Type: TypeTime},
			{Name: "field3", Type: TypeString},
			{Name: "field4", Type: TypeUUID},
			{Name: "field5", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"field1": now.Add(-1 * time.Hour),
		"field2": now.Add(-2 * time.Hour),
		"field3": "some-string-value",
		"field4": uuid.New(),
		"field5": int64(12345),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkEncodeCursor_TenFields benchmarks EncodeCursor with 10 fields (extreme case).
// Measures: payload size impact on performance and memory.
func BenchmarkEncodeCursor_TenFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_large",
		Sort: []SortField{
			{Name: "f1", Type: TypeTime},
			{Name: "f2", Type: TypeTime},
			{Name: "f3", Type: TypeString},
			{Name: "f4", Type: TypeString},
			{Name: "f5", Type: TypeUUID},
			{Name: "f6", Type: TypeUUID},
			{Name: "f7", Type: TypeInt64},
			{Name: "f8", Type: TypeInt64},
			{Name: "f9", Type: TypeFloat64},
			{Name: "f10", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"f1":  now.Add(-1 * time.Hour),
		"f2":  now.Add(-2 * time.Hour),
		"f3":  "string-1",
		"f4":  "string-2",
		"f5":  uuid.New(),
		"f6":  uuid.New(),
		"f7":  int64(111),
		"f8":  int64(222),
		"f9":  3.14,
		"f10": int64(333),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// =============================
// DECODECURSOR BENCHMARKS
// =============================

// BenchmarkDecodeCursor_ThreeFields benchmarks DecodeCursor with 3 fields.
// Measures: base64 decode, HMAC verification, JSON unmarshal overhead.
func BenchmarkDecodeCursor_ThreeFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_posts",
		Sort: []SortField{
			{Name: "published_at", Type: TypeTime},
			{Name: "created_at", Type: TypeTime},
			{Name: "id", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"published_at": now.Add(-1 * time.Hour),
		"created_at":   now.Add(-2 * time.Hour),
		"id":           int64(99999),
	}
	token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkDecodeCursor_FiveFields benchmarks DecodeCursor with 5 fields.
// Measures: larger payload decode performance.
func BenchmarkDecodeCursor_FiveFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_complex",
		Sort: []SortField{
			{Name: "field1", Type: TypeTime},
			{Name: "field2", Type: TypeTime},
			{Name: "field3", Type: TypeString},
			{Name: "field4", Type: TypeUUID},
			{Name: "field5", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"field1": now.Add(-1 * time.Hour),
		"field2": now.Add(-2 * time.Hour),
		"field3": "some-string-value",
		"field4": uuid.New(),
		"field5": int64(12345),
	}
	token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkDecodeCursor_TenFields benchmarks DecodeCursor with 10 fields (extreme case).
// Measures: extreme payload decode performance.
func BenchmarkDecodeCursor_TenFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_large",
		Sort: []SortField{
			{Name: "f1", Type: TypeTime},
			{Name: "f2", Type: TypeTime},
			{Name: "f3", Type: TypeString},
			{Name: "f4", Type: TypeString},
			{Name: "f5", Type: TypeUUID},
			{Name: "f6", Type: TypeUUID},
			{Name: "f7", Type: TypeInt64},
			{Name: "f8", Type: TypeInt64},
			{Name: "f9", Type: TypeFloat64},
			{Name: "f10", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"f1":  now.Add(-1 * time.Hour),
		"f2":  now.Add(-2 * time.Hour),
		"f3":  "string-1",
		"f4":  "string-2",
		"f5":  uuid.New(),
		"f6":  uuid.New(),
		"f7":  int64(111),
		"f8":  int64(222),
		"f9":  3.14,
		"f10": int64(333),
	}
	token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// =============================
// ROUNDTRIP BENCHMARKS
// =============================

// BenchmarkCursorRoundTrip_ThreeFields benchmarks encode + decode roundtrip with 3 fields.
// Measures: full cursor lifecycle cost (encode + decode).
func BenchmarkCursorRoundTrip_ThreeFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_posts",
		Sort: []SortField{
			{Name: "published_at", Type: TypeTime},
			{Name: "created_at", Type: TypeTime},
			{Name: "id", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"published_at": now.Add(-1 * time.Hour),
		"created_at":   now.Add(-2 * time.Hour),
		"id":           int64(99999),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkCursorRoundTrip_FiveFields benchmarks encode + decode roundtrip with 5 fields.
func BenchmarkCursorRoundTrip_FiveFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_complex",
		Sort: []SortField{
			{Name: "field1", Type: TypeTime},
			{Name: "field2", Type: TypeTime},
			{Name: "field3", Type: TypeString},
			{Name: "field4", Type: TypeUUID},
			{Name: "field5", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"field1": now.Add(-1 * time.Hour),
		"field2": now.Add(-2 * time.Hour),
		"field3": "some-string-value",
		"field4": uuid.New(),
		"field5": int64(12345),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// =============================
// SIGNATURE VERIFICATION BENCHMARKS
// =============================

// BenchmarkDecodeCursor_SignatureVerification measures HMAC verification overhead.
// Uses a valid token where only signature verification is measured.
func BenchmarkDecodeCursor_SignatureVerification(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_posts",
		Sort: []SortField{
			{Name: "published_at", Type: TypeTime},
			{Name: "created_at", Type: TypeTime},
			{Name: "id", Type: TypeInt64},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"published_at": now.Add(-1 * time.Hour),
		"created_at":   now.Add(-2 * time.Hour),
		"id":           int64(99999),
	}
	token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))

	// Verify it's a valid base64 token
	_, err := base64.RawURLEncoding.DecodeString(token)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// =============================
// ENCODE/DECODE BY TYPE
// =============================

// BenchmarkEncodeCursor_AllTimeFields benchmarks cursor with all time.Time fields (max timestamp overhead).
// Measures: encoding performance with dense timestamp payloads.
func BenchmarkEncodeCursor_AllTimeFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_times",
		Sort: []SortField{
			{Name: "t1", Type: TypeTime},
			{Name: "t2", Type: TypeTime},
			{Name: "t3", Type: TypeTime},
			{Name: "t4", Type: TypeTime},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"t1": now.Add(-1 * time.Hour),
		"t2": now.Add(-2 * time.Hour),
		"t3": now.Add(-3 * time.Hour),
		"t4": now.Add(-4 * time.Hour),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkDecodeCursor_AllTimeFields benchmarks decode with all time.Time fields.
func BenchmarkDecodeCursor_AllTimeFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_times",
		Sort: []SortField{
			{Name: "t1", Type: TypeTime},
			{Name: "t2", Type: TypeTime},
			{Name: "t3", Type: TypeTime},
			{Name: "t4", Type: TypeTime},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"t1": now.Add(-1 * time.Hour),
		"t2": now.Add(-2 * time.Hour),
		"t3": now.Add(-3 * time.Hour),
		"t4": now.Add(-4 * time.Hour),
	}
	token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkEncodeCursor_AllUUIDFields benchmarks cursor with all UUID fields.
// Measures: encoding performance with dense UUID payloads.
func BenchmarkEncodeCursor_AllUUIDFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_uuids",
		Sort: []SortField{
			{Name: "id1", Type: TypeUUID},
			{Name: "id2", Type: TypeUUID},
			{Name: "id3", Type: TypeUUID},
			{Name: "id4", Type: TypeUUID},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"id1": uuid.New(),
		"id2": uuid.New(),
		"id3": uuid.New(),
		"id4": uuid.New(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

// BenchmarkDecodeCursor_AllUUIDFields benchmarks decode with all UUID fields.
func BenchmarkDecodeCursor_AllUUIDFields(b *testing.B) {
	cfg := CursorConfig{
		Kind: "bench_uuids",
		Sort: []SortField{
			{Name: "id1", Type: TypeUUID},
			{Name: "id2", Type: TypeUUID},
			{Name: "id3", Type: TypeUUID},
			{Name: "id4", Type: TypeUUID},
		},
		TTL: time.Hour,
	}
	signer := NewSigner([]byte("bench-key-32-bytes-long-secret"), nil)
	now := time.Now().UTC()
	fields := map[string]any{
		"id1": uuid.New(),
		"id2": uuid.New(),
		"id3": uuid.New(),
		"id4": uuid.New(),
	}
	token, _ := EncodeCursor(cfg, fields, WithSigner(signer), WithNow(func() time.Time { return now }))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = DecodeCursor(cfg, token, WithSigner(signer), WithNow(func() time.Time { return now }))
	}
}

package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
)

const (
	// payloadFieldKind is the cursor payload field carrying endpoint kind.
	payloadFieldKind = "kind"
	// payloadFieldIssuedAt carries int64 unix seconds UTC.
	payloadFieldIssuedAt = "issued_at"
	// payloadFieldFields carries the row tuple map keyed by SortField.Name.
	payloadFieldFields = "fields"
)

var (
	b64 = base64.RawURLEncoding
)

// Option tunes one-off behaviour in EncodeCursor / DecodeCursor / ParseRequest.
type Option func(*options)

type options struct {
	signer    *Signer
	nowFn     func() time.Time
	pageReqID string
}

// WithSigner overrides the Signer for one call. Tests use this to inject a
// fixed key (avoiding env) or rotate PreviousKey scenarios.
func WithSigner(s *Signer) Option {
	return func(o *options) { o.signer = s }
}

// WithNow injects a mockable clock for expiry tests.
func WithNow(fn func() time.Time) Option {
	return func(o *options) { o.nowFn = fn }
}

func applyOptions(opts []Option) (*options, error) {
	out := &options{nowFn: time.Now}
	for _, fn := range opts {
		fn(out)
	}
	if out.signer == nil {
		g, err := GlobalSigner()
		if err != nil {
			return nil, err
		}
		out.signer = g
	}
	return out, nil
}

// EncodeCursor produces a URL-safe, opaque cursor string carrying the
// endpoint kind (cfg.Kind), an issued-at timestamp, and the caller-supplied
// sort field values (fields). Fields are type-normalized via
// normalizeSortValue using cfg.Sort before serialisation.
//
// The returned token is safe to hand directly to callers as a query
// parameter; it is base64url-encoded (no padding) and HMAC-signed.
func EncodeCursor(cfg CursorConfig, fields map[string]any, opts ...Option) (string, error) {
	opt, err := applyOptions(opts)
	if err != nil {
		return "", err
	}
	normalized := make(map[string]any, len(cfg.Sort))
	for _, f := range cfg.Sort {
		v, ok := fields[f.Name]
		if !ok {
			return "", fmt.Errorf("%w: %s", ErrCursorMissingField, f.Name)
		}
		normalized[f.Name] = normalizeSortValue(f.Type, v)
	}
	payload := map[string]any{
		payloadFieldKind:     cfg.Kind,
		payloadFieldIssuedAt: opt.nowFn().UTC().Unix(),
		payloadFieldFields:   normalized,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCursorMalformed, err)
	}
	signed := opt.signer.Sign(raw)
	return b64.EncodeToString(signed), nil
}

// DecodeCursor verifies the HMAC signature on token, checks that its kind
// matches cfg.Kind, rejects tokens older than the endpoint TTL, and returns
// the sort field map along with the issued-at timestamp. Validation against
// declared sort field types / presence is performed by ValidateCursor.
func DecodeCursor(cfg CursorConfig, token string, opts ...Option) (map[string]any, time.Time, error) {
	opt, err := applyOptions(opts)
	if err != nil {
		return nil, time.Time{}, err
	}
	signed, err := b64.DecodeString(token)
	if err != nil {
		return nil, time.Time{}, ErrCursorMalformed
	}
	raw, err := opt.signer.Verify(signed)
	if err != nil {
		return nil, time.Time{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, time.Time{}, ErrCursorMalformed
	}
	kind, _ := payload[payloadFieldKind].(string)
	if kind != cfg.Kind {
		return nil, time.Time{}, fmt.Errorf("%w: got %q want %q", ErrCursorWrongKind, kind, cfg.Kind)
	}
	var issuedAt time.Time
	switch v := payload[payloadFieldIssuedAt].(type) {
	case float64:
		issuedAt = time.Unix(int64(v), 0).UTC()
	case int64:
		issuedAt = time.Unix(v, 0).UTC()
	case int:
		issuedAt = time.Unix(int64(v), 0).UTC()
	default:
		return nil, time.Time{}, fmt.Errorf("%w: issued_at", ErrCursorMalformed)
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if opt.nowFn().UTC().Sub(issuedAt) > ttl {
		return nil, issuedAt, ErrCursorExpired
	}
	fields, _ := payload[payloadFieldFields].(map[string]any)
	if fields == nil {
		return nil, issuedAt, fmt.Errorf("%w: missing fields", ErrCursorMalformed)
	}
	for _, f := range cfg.Sort {
		if raw, ok := fields[f.Name]; ok {
			fields[f.Name] = normalizeSortValue(f.Type, raw)
		}
	}
	return fields, issuedAt, nil
}

// ValidateCursor performs structural validation of decoded cursor fields
// against the endpoint's CursorConfig: every SortField must be present and
// have the declared runtime type (accepting numeric coercions common to
// JSON decode, e.g. float64 for int64).
func ValidateCursor(cfg CursorConfig, fields map[string]any) error {
	for _, f := range cfg.Sort {
		v, ok := fields[f.Name]
		if !ok || v == nil {
			return fmt.Errorf("%w: %s", ErrCursorMissingField, f.Name)
		}
		if err := checkType(f.Type, v); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrCursorFieldType, f.Name, err)
		}
	}
	return nil
}

// checkType returns nil if v has the declared ColumnType (including standard
// JSON numeric widening). JSON decode always turns numbers into float64, so
// we accept float64 for TypeInt64 when it has no fractional part.
func checkType(t ColumnType, v any) error {
	switch t {
	case TypeInt64:
		switch n := v.(type) {
		case int, int32, int64, uint, uint32, uint64:
			return nil
		case float64:
			if n == float64(int64(n)) {
				return nil
			}
			return errors.New("float value is not integer")
		case json.Number:
			if _, err := n.Int64(); err == nil {
				return nil
			}
			return fmt.Errorf("json.Number not int64: %v", n)
		}
		return fmt.Errorf("got %s", reflect.TypeOf(v))
	case TypeFloat64:
		switch n := v.(type) {
		case float32, float64:
			return nil
		case int, int32, int64, uint, uint32, uint64:
			return nil
		case json.Number:
			if _, err := n.Float64(); err == nil {
				return nil
			}
			return fmt.Errorf("json.Number not float64: %v", n)
		}
		return fmt.Errorf("got %s", reflect.TypeOf(v))
	case TypeString:
		if _, ok := v.(string); ok {
			return nil
		}
		return fmt.Errorf("got %s", reflect.TypeOf(v))
	case TypeUUID:
		switch u := v.(type) {
		case uuid.UUID:
			return nil
		case string:
			_, err := uuid.Parse(u)
			return err
		}
		return fmt.Errorf("got %s", reflect.TypeOf(v))
	case TypeTime:
		switch tm := v.(type) {
		case time.Time:
			return nil
		case string:
			_, err := time.Parse(time.RFC3339Nano, tm)
			return err
		}
		return fmt.Errorf("got %s", reflect.TypeOf(v))
	}
	return fmt.Errorf("unknown column type %d", t)
}

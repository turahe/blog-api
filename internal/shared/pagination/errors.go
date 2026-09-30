package pagination

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by DecodeCursor / ValidateCursor / ParseRequest.
//
// Handlers convert these into HTTP 400 responses with the matching
// error.code strings so frontends can switch on stable machine-readable
// identifiers. Use [ErrorCode] to retrieve the code; use [ErrorCause] for
// the human-facing message.
var (
	// ErrCursorMalformed means the token could not be base64url-decoded or
	// was otherwise syntactically invalid.
	ErrCursorMalformed = errors.New("pagination: cursor is malformed")

	// ErrCursorInvalidSignature means the HMAC failed verification.
	// Cursors are signed with PAGINATION_HMAC_KEY (or APP_KEY).
	ErrCursorInvalidSignature = errors.New("pagination: cursor signature is invalid")

	// ErrCursorExpired means the cursor's issued_at timestamp exceeds the
	// endpoint's TTL (default 24h; per-endpoint override in CursorConfig).
	ErrCursorExpired = errors.New("pagination: cursor has expired")

	// ErrCursorWrongKind means the cursor was issued for a different
	// endpoint kind (e.g. posts_public cursor presented to comments_newest).
	ErrCursorWrongKind = errors.New("pagination: cursor kind does not match endpoint")

	// ErrCursorMissingField means a required sort field declared in the
	// endpoint's CursorConfig is not present in the decoded payload.
	ErrCursorMissingField = errors.New("pagination: cursor missing required sort field")

	// ErrCursorFieldType means a sort field value had the wrong runtime type
	// for the declared ColumnType (string for int64 column, etc.).
	ErrCursorFieldType = errors.New("pagination: cursor sort field has wrong type")

	// ErrCursorUnsupported means the endpoint does not support cursor mode
	// (e.g. offset-only search rank endpoints; rare).
	ErrCursorUnsupported = errors.New("pagination: cursor mode not supported for this endpoint")
)

// Stable error.code values written to the envelope for every sentinel above.
// These strings are public API; do not rename without a version bump.
const (
	CodeMalformed    = "pagination.cursor.malformed"
	CodeTampered     = "pagination.cursor.tampered"
	CodeExpired      = "pagination.cursor.expired"
	CodeWrongKind    = "pagination.cursor.wrong_kind"
	CodeMissingField = "pagination.cursor.missing_field"
	CodeFieldType    = "pagination.cursor.field_type"
	CodeUnsupported  = "pagination.cursor.unsupported"
)

// ErrorCode maps each sentinel error to its public error.code string.
// Unknown errors return "pagination.cursor.malformed" as a safe default.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrCursorMalformed):
		return CodeMalformed
	case errors.Is(err, ErrCursorInvalidSignature):
		return CodeTampered
	case errors.Is(err, ErrCursorExpired):
		return CodeExpired
	case errors.Is(err, ErrCursorWrongKind):
		return CodeWrongKind
	case errors.Is(err, ErrCursorMissingField):
		return CodeMissingField
	case errors.Is(err, ErrCursorFieldType):
		return CodeFieldType
	case errors.Is(err, ErrCursorUnsupported):
		return CodeUnsupported
	default:
		return CodeMalformed
	}
}

// ErrorCause returns the human-readable message for err. If err wraps a
// sentinel, the wrapped message is retained; otherwise err.Error() is used.
func ErrorCause(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("The cursor could not be processed: %v.", err)
}

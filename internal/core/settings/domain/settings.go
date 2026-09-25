// Package domain defines the settings catalogue, typed validation, and change history.
//
// The catalogue in code is the source of truth for which keys exist and how they are
// validated; storage only holds values that were changed from their coded default.
package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ValueType is the JSON type a setting holds.
type ValueType string

// Value types.
const (
	TypeString     ValueType = "string"
	TypeInteger    ValueType = "integer"
	TypeBoolean    ValueType = "boolean"
	TypeStringList ValueType = "string_array"
)

// Sensitivity controls where a setting may be shown.
type Sensitivity string

// Sensitivity levels. ServerOnly keys are never returned or written over HTTP.
const (
	PublicSafe Sensitivity = "public_safe"
	AdminOnly  Sensitivity = "admin_only"
	ServerOnly Sensitivity = "server_only"
)

// Category groups keys for validation and UI.
type Category string

// Categories.
const (
	CategorySite          Category = "site"
	CategoryContent       Category = "content"
	CategoryMedia         Category = "media"
	CategoryAnalytics     Category = "analytics"
	CategoryNotifications Category = "notifications"
	CategorySEO           Category = "seo"
	CategorySecurity      Category = "security"
)

// Categories lists every category in display order.
var Categories = []Category{
	CategorySite, CategoryContent, CategoryMedia, CategoryAnalytics,
	CategoryNotifications, CategorySEO, CategorySecurity,
}

// ValidCategory reports whether c is a known category.
func ValidCategory(c Category) bool {
	return slices.Contains(Categories, c)
}

// Violation reasons.
const (
	ReasonUnknownKey   = "unknown_key"
	ReasonReadOnly     = "read_only"
	ReasonDuplicate    = "duplicate_key"
	ReasonTypeMismatch = "type_mismatch"
	ReasonOutOfRange   = "out_of_range"
	ReasonTooLong      = "too_long"
	ReasonNotAllowed   = "not_allowed"
	ReasonFormat       = "invalid_format"
	ReasonTooMany      = "too_many_items"
)

// Violation explains why one submitted value was rejected.
type Violation struct {
	Key     string `json:"key"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// ValidationError rejects a whole update; no key is applied.
type ValidationError struct {
	Violations []Violation
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("settings validation failed: %d violation(s)", len(e.Violations))
}

// ErrVersionConflict means a key changed since the caller read it.
var ErrVersionConflict = errors.New("settings version conflict")

// Definition declares one key: its type, default, sensitivity, and value rules.
type Definition struct {
	Key         string
	Category    Category
	Type        ValueType
	Sensitivity Sensitivity
	Description string
	// Default is the value used while no row is stored: string, int64, bool, or []string.
	Default any

	// MaxLength bounds strings and list items in characters; zero means unbounded.
	MaxLength int
	// Enum restricts strings and list items to these values.
	Enum []string
	// Pattern must match non-empty strings and list items.
	Pattern *regexp.Regexp
	// Min and Max bound integers (inclusive).
	Min, Max int64
	// MaxItems bounds list length; zero means unbounded.
	MaxItems int
	// Check runs after the type rules and returns a reason and message, or "" when valid.
	Check func(value any) (reason, message string)
}

// Decode parses raw JSON into the key's Go value (string, int64, bool, or []string)
// and applies its rules. Type coercion is never applied: "true" is not a boolean.
func (d Definition) Decode(raw json.RawMessage) (any, *Violation) {
	value, v := d.decodeType(raw)
	if v != nil {
		return nil, v
	}

	if v := d.checkRules(value); v != nil {
		return nil, v
	}

	if d.Check != nil {
		if reason, message := d.Check(value); reason != "" {
			return nil, d.violation(reason, message)
		}
	}

	return value, nil
}

func (d Definition) decodeType(raw json.RawMessage) (any, *Violation) {
	raw = bytes.TrimSpace(raw)
	mismatch := d.violation(ReasonTypeMismatch, "value must be "+article(d.Type))

	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, mismatch
	}

	var (
		value any
		ok    bool
	)

	switch d.Type {
	case TypeString:
		value, ok = decodeAs[string](raw)
	case TypeInteger:
		value, ok = decodeInteger(raw)
	case TypeBoolean:
		value, ok = decodeAs[bool](raw)
	case TypeStringList:
		value, ok = decodeAs[[]string](raw)
	}

	if !ok {
		return nil, mismatch
	}

	return value, nil
}

func decodeAs[T any](raw json.RawMessage) (any, bool) {
	var v T
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}

	return v, true
}

func decodeInteger(raw json.RawMessage) (any, bool) {
	// json.Number also accepts a quoted number, which would be coercion.
	if raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return nil, false
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var n json.Number
	if dec.Decode(&n) != nil {
		return nil, false
	}

	i, err := n.Int64()

	return i, err == nil
}

func (d Definition) checkRules(value any) *Violation {
	switch v := value.(type) {
	case string:
		return d.checkString(v, "value")
	case int64:
		if v < d.Min || v > d.Max {
			return d.violation(ReasonOutOfRange, fmt.Sprintf("value must be between %d and %d", d.Min, d.Max))
		}
	case []string:
		return d.checkList(v)
	}

	return nil
}

func (d Definition) checkList(list []string) *Violation {
	if d.MaxItems > 0 && len(list) > d.MaxItems {
		return d.violation(ReasonTooMany, fmt.Sprintf("at most %d items allowed", d.MaxItems))
	}

	seen := make(map[string]struct{}, len(list))
	for i, item := range list {
		if v := d.checkString(item, fmt.Sprintf("item %d", i)); v != nil {
			return v
		}

		if _, dup := seen[item]; dup {
			return d.violation(ReasonNotAllowed, fmt.Sprintf("item %q is repeated", item))
		}

		seen[item] = struct{}{}
	}

	return nil
}

func (d Definition) checkString(s, what string) *Violation {
	if d.MaxLength > 0 && utf8.RuneCountInString(s) > d.MaxLength {
		return d.violation(ReasonTooLong, fmt.Sprintf("%s must be at most %d characters", what, d.MaxLength))
	}

	if strings.ContainsFunc(s, isControl) {
		return d.violation(ReasonFormat, what+" must not contain control characters")
	}

	if len(d.Enum) > 0 && !slices.Contains(d.Enum, s) {
		return d.violation(ReasonNotAllowed, fmt.Sprintf("%s must be one of: %s", what, strings.Join(d.Enum, ", ")))
	}

	if d.Pattern != nil && s != "" && !d.Pattern.MatchString(s) {
		return d.violation(ReasonFormat, what+" has an invalid format")
	}

	return nil
}

func (d Definition) violation(reason, message string) *Violation {
	return &Violation{Key: d.Key, Reason: reason, Message: message}
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

func article(t ValueType) string {
	switch t {
	case TypeInteger:
		return "an integer"
	case TypeStringList:
		return "an array of strings"
	case TypeString, TypeBoolean:
		return "a " + string(t)
	default:
		return "a " + string(t)
	}
}

// Equal reports whether two decoded values are the same.
func Equal(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

// Stored is a persisted value; Version starts at 1 and grows by one per change.
type Stored struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	UpdatedBy *uuid.UUID      `json:"updated_by,omitempty"`
}

// Setting is a key resolved against the catalogue.
type Setting struct {
	Definition

	Value any
	// Version is 0 while the coded default applies.
	Version int64
	// Defaulted is true when no valid stored value exists.
	Defaulted bool
	UpdatedAt *time.Time
	UpdatedBy *uuid.UUID
}

// Write is one conditional change. The store applies it only while the key is still at
// ExpectedVersion (0 means no row yet) and appends a history row.
type Write struct {
	Key             string
	Previous        json.RawMessage
	Value           json.RawMessage
	ExpectedVersion int64
	ChangedBy       *uuid.UUID
	RequestID       string
	HistoryID       uuid.UUID
	At              time.Time
}

// Change summarises one applied key.
type Change struct {
	Key         string
	Previous    any
	New         any
	Version     int64
	Sensitivity Sensitivity
}

// HistoryEntry is one recorded change. Values are nil when Redacted.
type HistoryEntry struct {
	UUID      uuid.UUID
	Key       string
	Previous  json.RawMessage
	New       json.RawMessage
	Version   int64
	ChangedBy *uuid.UUID
	RequestID string
	CreatedAt time.Time
	Redacted  bool
}

// HistoryFilter selects history rows, newest first.
type HistoryFilter struct {
	Key     string
	Page    int
	PerPage int
}

// HistoryPage is one page of history.
type HistoryPage struct {
	Items   []HistoryEntry
	Page    int
	PerPage int
	Total   int64
}

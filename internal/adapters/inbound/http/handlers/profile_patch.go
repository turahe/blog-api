package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const maxProfilePatchBytes = 64 << 10

var (
	errUnknownProfileField     = errors.New("unknown profile field")
	errUnsupportedProfileField = errors.New("unsupported profile field")
	errProfileFieldType        = errors.New("invalid profile field type")
)

// unsupportedProfileFields are allowlisted by the spec but not stored yet.
var unsupportedProfileFields = map[string]string{
	"contact_phone": "contact_phone is not supported yet",
}

// decodeProfilePatch parses an allowlisted profile patch, keeping "absent" and
// "null" apart so null can clear a field.
func decodeProfilePatch(body io.Reader) (userdomain.ProfilePatch, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxProfilePatchBytes+1))
	if err != nil {
		return userdomain.ProfilePatch{}, err
	}

	if len(raw) > maxProfilePatchBytes {
		return userdomain.ProfilePatch{}, fmt.Errorf("%w: body exceeds %d bytes", errProfileFieldType, maxProfilePatchBytes)
	}

	fields, err := decodeObject(raw, "body")
	if err != nil {
		return userdomain.ProfilePatch{}, err
	}

	var patch userdomain.ProfilePatch

	textFields := map[string]*userdomain.Change[string]{
		"full_name":        &patch.FullName,
		"display_name":     &patch.DisplayName,
		"bio":              &patch.Bio,
		"contact_website":  &patch.ContactWebsite,
		"contact_location": &patch.ContactLocation,
		"locale":           &patch.Locale,
		"timezone":         &patch.Timezone,
	}

	for _, key := range sortedKeys(fields) {
		value := fields[key]

		switch dst, ok := textFields[key]; {
		case ok:
			err = decodeChange(value, key, dst)
		case key == "marketing_consent":
			err = decodeChange(value, key, &patch.MarketingConsent)
		case key == "social_links":
			err = decodeSocialLinks(value, &patch)
		case unsupportedProfileFields[key] != "":
			err = fmt.Errorf("%w: %s", errUnsupportedProfileField, unsupportedProfileFields[key])
		default:
			err = fmt.Errorf("%w: %s", errUnknownProfileField, key)
		}

		if err != nil {
			return userdomain.ProfilePatch{}, err
		}
	}

	return patch, nil
}

func decodeSocialLinks(raw json.RawMessage, patch *userdomain.ProfilePatch) error {
	links := map[string]*userdomain.Change[string]{
		"twitter":  &patch.Twitter,
		"linkedin": &patch.LinkedIn,
		"github":   &patch.GitHub,
	}

	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		for _, change := range links {
			*change = userdomain.Change[string]{Set: true}
		}

		return nil
	}

	fields, err := decodeObject(raw, "social_links")
	if err != nil {
		return err
	}

	for _, key := range sortedKeys(fields) {
		dst, ok := links[key]
		if !ok {
			return fmt.Errorf("%w: social_links.%s", errUnknownProfileField, key)
		}

		if err := decodeChange(fields[key], "social_links."+key, dst); err != nil {
			return err
		}
	}

	return nil
}

func decodeObject(raw []byte, name string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%w: %s must be a JSON object", errProfileFieldType, name)
	}

	return fields, nil
}

func decodeChange[T any](raw json.RawMessage, key string, dst *userdomain.Change[T]) error {
	dst.Set = true

	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}

	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("%w: %s has the wrong type", errProfileFieldType, key)
	}

	dst.Value = &value

	return nil
}

func sortedKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

package service

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // timezone validation must not depend on the host's zoneinfo
	"unicode"
	"unicode/utf8"

	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

const (
	maxFullNameRunes    = 120
	maxDisplayNameRunes = 60
	maxBioRunes         = 4000
	maxLocationRunes    = 120
	maxURLBytes         = 2048
)

var (
	displayNamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._'-]*$`)
	localePattern      = regexp.MustCompile(`^[a-z]{2,3}(_[A-Z]{2})?$`)
	twitterPattern     = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)
	githubPattern      = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	linkedinPattern    = regexp.MustCompile(`^[A-Za-z0-9-]{3,100}$`)
)

// applyPatch validates patch and applies it to profile. It returns the new full
// name when the patch changes it.
func applyPatch(profile *userdomain.Profile, patch userdomain.ProfilePatch, now time.Time) (*string, error) {
	fullName, err := patchFullName(patch.FullName)
	if err != nil {
		return nil, err
	}

	steps := []func() error{
		func() error { return patchOptional(&profile.DisplayName, patch.DisplayName, cleanDisplayName) },
		func() error { return patchBio(&profile.Bio, patch.Bio) },
		func() error { return patchOptional(&profile.ContactWebsite, patch.ContactWebsite, cleanWebsite) },
		func() error { return patchOptional(&profile.ContactLocation, patch.ContactLocation, cleanLocation) },
		func() error { return patchHandle(&profile.Social.Twitter, patch.Twitter, "twitter", twitterPattern) },
		func() error {
			return patchHandle(&profile.Social.LinkedIn, patch.LinkedIn, "linkedin", linkedinPattern)
		},
		func() error { return patchHandle(&profile.Social.GitHub, patch.GitHub, "github", githubPattern) },
		func() error { return patchRequired(&profile.Locale, patch.Locale, "locale", cleanLocale) },
		func() error { return patchRequired(&profile.Timezone, patch.Timezone, "timezone", cleanTimezone) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}

	if patch.MarketingConsent.Set {
		if patch.MarketingConsent.Value == nil {
			return nil, invalid("marketing_consent must be true or false")
		}

		if *patch.MarketingConsent.Value != profile.MarketingConsent || profile.MarketingConsentUpdatedAt == nil {
			profile.MarketingConsent = *patch.MarketingConsent.Value
			profile.MarketingConsentUpdatedAt = &now
		}
	}

	return fullName, nil
}

// isEmptyPatch reports whether the patch changes nothing.
func isEmptyPatch(patch userdomain.ProfilePatch) bool {
	return !patch.FullName.Set && !patch.DisplayName.Set && !patch.Bio.Set &&
		!patch.ContactWebsite.Set && !patch.ContactLocation.Set &&
		!patch.Twitter.Set && !patch.LinkedIn.Set && !patch.GitHub.Set &&
		!patch.Locale.Set && !patch.Timezone.Set && !patch.MarketingConsent.Set
}

func patchFullName(change userdomain.Change[string]) (*string, error) {
	if !change.Set {
		return nil, nil
	}

	if change.Value == nil {
		return nil, invalid("full_name cannot be null")
	}

	name := strings.TrimSpace(*change.Value)
	if name == "" || utf8.RuneCountInString(name) > maxFullNameRunes || hasControl(name, false) {
		return nil, invalid(fmt.Sprintf("full_name must be 1-%d characters without control characters", maxFullNameRunes))
	}

	return &name, nil
}

// patchOptional applies a nullable field; null or blank clears it.
func patchOptional(dst **string, change userdomain.Change[string], clean func(string) (string, error)) error {
	if !change.Set {
		return nil
	}

	if change.Value == nil || strings.TrimSpace(*change.Value) == "" {
		*dst = nil
		return nil
	}

	value, err := clean(strings.TrimSpace(*change.Value))
	if err != nil {
		return err
	}

	*dst = &value

	return nil
}

func patchRequired(dst *string, change userdomain.Change[string], field string, clean func(string) (string, error)) error {
	if !change.Set {
		return nil
	}

	if change.Value == nil {
		return invalid(field + " cannot be null")
	}

	value, err := clean(strings.TrimSpace(*change.Value))
	if err != nil {
		return err
	}

	*dst = value

	return nil
}

func patchBio(dst *string, change userdomain.Change[string]) error {
	if !change.Set {
		return nil
	}

	if change.Value == nil {
		*dst = ""
		return nil
	}

	bio := strings.TrimSpace(*change.Value)
	if utf8.RuneCountInString(bio) > maxBioRunes || hasControl(bio, true) {
		return invalid(fmt.Sprintf("bio must be at most %d characters of plain text", maxBioRunes))
	}

	*dst = bio

	return nil
}

func patchHandle(dst *string, change userdomain.Change[string], field string, pattern *regexp.Regexp) error {
	if !change.Set {
		return nil
	}

	if change.Value == nil {
		*dst = ""
		return nil
	}

	handle := strings.TrimPrefix(strings.TrimSpace(*change.Value), "@")
	if handle != "" && !pattern.MatchString(handle) {
		return invalid(fmt.Sprintf("social_links.%s is not a valid handle", field))
	}

	*dst = handle

	return nil
}

func cleanDisplayName(name string) (string, error) {
	if utf8.RuneCountInString(name) > maxDisplayNameRunes || !displayNamePattern.MatchString(name) {
		return "", invalid(fmt.Sprintf("display_name must be 1-%d letters, digits, spaces, or . _ ' -", maxDisplayNameRunes))
	}

	return name, nil
}

func cleanWebsite(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > maxURLBytes || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil {
		return "", invalid("contact_website must be an absolute http(s) URL without credentials")
	}

	return parsed.String(), nil
}

func cleanLocation(location string) (string, error) {
	if utf8.RuneCountInString(location) > maxLocationRunes || hasControl(location, false) {
		return "", invalid(fmt.Sprintf("contact_location must be at most %d characters", maxLocationRunes))
	}

	return location, nil
}

func cleanLocale(locale string) (string, error) {
	locale = strings.ReplaceAll(locale, "-", "_")
	if !localePattern.MatchString(locale) {
		return "", invalid("locale must look like en or en_US")
	}

	return locale, nil
}

func cleanTimezone(zone string) (string, error) {
	if zone == "" || zone == "Local" {
		return "", invalid("timezone must be an IANA zone such as Asia/Jakarta")
	}

	if _, err := time.LoadLocation(zone); err != nil {
		return "", invalid("timezone must be an IANA zone such as Asia/Jakarta")
	}

	return zone, nil
}

// hasControl reports control characters; multiline allows newlines and tabs.
func hasControl(s string, multiline bool) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		if multiline && (r == '\n' || r == '\t' || r == '\r') {
			return false
		}

		return unicode.IsControl(r)
	})
}

func invalid(msg string) error {
	return fmt.Errorf("%w: %s", userdomain.ErrProfileValidation, msg)
}

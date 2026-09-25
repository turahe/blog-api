package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Profile errors.
var (
	ErrProfileValidation = errors.New("profile validation failed")
	ErrDisplayNameTaken  = errors.New("display name already taken")
)

// Visibility controls who can read a public profile.
type Visibility string

// Profile visibility levels.
const (
	VisibilityPublic   Visibility = "public"
	VisibilityUnlisted Visibility = "unlisted"
	VisibilityPrivate  Visibility = "private"
)

// SocialLinks are handles on external networks, stored without the leading @.
type SocialLinks struct {
	Twitter  string `json:"twitter,omitempty"`
	LinkedIn string `json:"linkedin,omitempty"`
	GitHub   string `json:"github,omitempty"`
}

// Profile is the editable presentation data attached 1:1 to a user.
type Profile struct {
	DisplayName               *string
	Bio                       string
	ContactWebsite            *string
	ContactLocation           *string
	Social                    SocialLinks
	Locale                    string
	Timezone                  string
	MarketingConsent          bool
	MarketingConsentUpdatedAt *time.Time
	UpdatedAt                 *time.Time
}

// DefaultProfile matches the column defaults used before a profile row exists.
func DefaultProfile() Profile {
	return Profile{Locale: "en_US", Timezone: "UTC"}
}

// Privacy controls what the public profile exposes.
type Privacy struct {
	Visibility    Visibility
	ShowEmail     bool
	ShowContact   bool
	AllowIndexing bool
}

// DefaultPrivacy matches the column defaults used before a settings row exists.
func DefaultPrivacy() Privacy {
	return Privacy{Visibility: VisibilityPublic, ShowContact: true, AllowIndexing: true}
}

// ErrPrivacyReauth reports a visibility reduction without a valid password proof.
var ErrPrivacyReauth = errors.New("privacy change requires password re-verification")

// Valid reports whether v is a known visibility level.
func (v Visibility) Valid() bool {
	return v.reach() >= 0
}

// Narrows reports whether next shows the profile to fewer viewers than v.
func (v Visibility) Narrows(next Visibility) bool {
	return next.reach() < v.reach()
}

func (v Visibility) reach() int {
	switch v {
	case VisibilityPrivate:
		return 0
	case VisibilityUnlisted:
		return 1
	case VisibilityPublic:
		return 2
	default:
		return -1
	}
}

// PrivacyPatch lists the privacy flags a caller may change; nil fields are kept.
type PrivacyPatch struct {
	Visibility    *Visibility
	ShowEmail     *bool
	ShowContact   *bool
	AllowIndexing *bool
}

// Empty reports whether the patch changes nothing.
func (p PrivacyPatch) Empty() bool {
	return p.Visibility == nil && p.ShowEmail == nil && p.ShowContact == nil && p.AllowIndexing == nil
}

// Apply returns p with the patch applied.
func (p Privacy) Apply(patch PrivacyPatch) (Privacy, error) {
	if patch.Visibility != nil {
		if !patch.Visibility.Valid() {
			return Privacy{}, fmt.Errorf("%w: visibility_profile must be public, unlisted, or private", ErrProfileValidation)
		}

		p.Visibility = *patch.Visibility
	}

	assignIf(&p.ShowEmail, patch.ShowEmail)
	assignIf(&p.ShowContact, patch.ShowContact)
	assignIf(&p.AllowIndexing, patch.AllowIndexing)

	return p, nil
}

func assignIf[T any](dst, src *T) {
	if src != nil {
		*dst = *src
	}
}

// ProfileView is a user with their profile, privacy settings, and avatar.
type ProfileView struct {
	User       User
	AvatarUUID *uuid.UUID
	Profile    Profile
	Privacy    Privacy
}

// Change is a tri-state patch field: Set=false leaves the value alone; Set=true
// applies Value, where nil clears nullable fields.
type Change[T any] struct {
	Set   bool
	Value *T
}

// ProfilePatch lists the allowlisted profile fields a caller may change.
type ProfilePatch struct {
	FullName         Change[string]
	DisplayName      Change[string]
	Bio              Change[string]
	ContactWebsite   Change[string]
	ContactLocation  Change[string]
	Twitter          Change[string]
	LinkedIn         Change[string]
	GitHub           Change[string]
	Locale           Change[string]
	Timezone         Change[string]
	MarketingConsent Change[bool]
}

package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

var (
	profileUserID   = uuid.MustParse("0198a1b2-0000-7000-8000-000000003001")
	profileAvatarID = uuid.MustParse("0198a1b2-0000-7000-8000-000000003002")
	profileAt       = time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
)

// completeProfile has every optional field set and a private profile that shows nothing extra.
func completeProfile() userdomain.ProfileView {
	return userdomain.ProfileView{
		User: userdomain.User{
			UUID:            profileUserID,
			Email:           "gopher@example.com",
			Username:        "gopher",
			FullName:        "Go Pher",
			PasswordHash:    "secret-hash",
			Status:          userdomain.StatusActive,
			EmailVerifiedAt: &profileAt,
			LastLoginAt:     &profileAt,
			LoginCount:      12,
			CreatedAt:       time.Date(2026, 1, 1, 7, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		},
		AvatarUUID: &profileAvatarID,
		Profile: userdomain.Profile{
			DisplayName:               new("Gopher"),
			Bio:                       "Writes Go.",
			ContactWebsite:            new("https://go.example"),
			ContactLocation:           new("Jakarta"),
			Social:                    userdomain.SocialLinks{Twitter: "gopher", GitHub: "gopher"},
			Locale:                    "id-ID",
			Timezone:                  "Asia/Jakarta",
			MarketingConsent:          true,
			MarketingConsentUpdatedAt: &profileAt,
			UpdatedAt:                 &profileAt,
		},
		Privacy: userdomain.Privacy{Visibility: userdomain.VisibilityPrivate},
	}
}

const completeProfileJSON = `"id": "0198a1b2-0000-7000-8000-000000003001", "username": "gopher",
	"email": "gopher@example.com", "emailVerifiedAt": "2026-08-01T10:00:00Z", "fullName": "Go Pher",
	"displayName": "Gopher", "bio": "Writes Go.", "avatarId": "0198a1b2-0000-7000-8000-000000003002",
	"contact": {"website": "https://go.example", "location": "Jakarta"},
	"socialLinks": {"twitter": "gopher", "linkedin": "", "github": "gopher"},
	"locale": "id-ID", "timezone": "Asia/Jakarta", "marketingConsent": true,
	"marketingConsentUpdatedAt": "2026-08-01T10:00:00Z",
	"privacy": {"visibilityProfile": "private", "visibilityEmail": false, "visibilityContact": false,
		"searchAllowIndexing": false},
	"createdAt": "2026-01-01T00:00:00Z", "profileUpdatedAt": "2026-08-01T10:00:00Z"`

const newProfileJSON = `"id": "0198a1b2-0000-7000-8000-000000003001", "username": "gopher", "email": "",
	"emailVerifiedAt": null, "fullName": "", "displayName": null, "bio": "", "avatarId": null,
	"contact": {"website": null, "location": null},
	"socialLinks": {"twitter": "", "linkedin": "", "github": ""},
	"locale": "", "timezone": "", "marketingConsent": false, "marketingConsentUpdatedAt": null,
	"privacy": {"visibilityProfile": "public", "visibilityEmail": false, "visibilityContact": false,
		"searchAllowIndexing": true},
	"createdAt": "2026-08-01T10:00:00Z", "profileUpdatedAt": null`

func newProfile() userdomain.ProfileView {
	return userdomain.ProfileView{
		User:    userdomain.User{UUID: profileUserID, Username: "gopher", Status: userdomain.StatusPending, CreatedAt: profileAt},
		Privacy: userdomain.Privacy{Visibility: userdomain.VisibilityPublic, AllowIndexing: true},
	}
}

func TestProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		view userdomain.ProfileView
		want string
	}{
		{name: "complete", view: completeProfile(), want: `{` + completeProfileJSON + `}`},
		{name: "new account", view: newProfile(), want: `{` + newProfileJSON + `}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Profile(tt.view))
		})
	}
}

func TestAdminProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		view userdomain.ProfileView
		want string
	}{
		{
			name: "active account",
			view: completeProfile(),
			want: `{` + completeProfileJSON + `, "status": "active", "lastLoginAt": "2026-08-01T10:00:00Z", "loginCount": 12}`,
		},
		{
			name: "never signed in",
			view: newProfile(),
			want: `{` + newProfileJSON + `, "status": "pending", "lastLoginAt": null, "loginCount": 0}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, AdminProfile(tt.view))
		})
	}
}

func TestPublicProfile(t *testing.T) {
	t.Parallel()

	const public = `"id": "0198a1b2-0000-7000-8000-000000003001", "username": "gopher", "fullName": "Go Pher",
		"displayName": "Gopher", "bio": "Writes Go.", "avatarId": "0198a1b2-0000-7000-8000-000000003002",
		"socialLinks": {"twitter": "gopher", "linkedin": "", "github": "gopher"}, "joinedAt": "2026-01-01T00:00:00Z"`

	withPrivacy := func(showContact, showEmail bool) userdomain.ProfileView {
		view := completeProfile()
		view.Privacy.ShowContact, view.Privacy.ShowEmail = showContact, showEmail

		return view
	}

	tests := []struct {
		name string
		view userdomain.ProfileView
		want string
	}{
		{name: "hides contact and email", view: withPrivacy(false, false), want: `{` + public + `}`},
		{
			name: "shows contact",
			view: withPrivacy(true, false),
			want: `{` + public + `, "contact": {"website": "https://go.example", "location": "Jakarta"}}`,
		},
		{name: "shows email", view: withPrivacy(false, true), want: `{` + public + `, "email": "gopher@example.com"}`},
		{
			name: "shows both",
			view: withPrivacy(true, true),
			want: `{` + public + `, "contact": {"website": "https://go.example", "location": "Jakarta"},
				"email": "gopher@example.com"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, PublicProfile(tt.view))
		})
	}
}

func TestPrivacy(t *testing.T) {
	t.Parallel()

	privacy := userdomain.Privacy{Visibility: userdomain.VisibilityUnlisted, ShowEmail: true, ShowContact: true, AllowIndexing: false}

	assertJSON(t, `{"visibilityProfile": "unlisted", "visibilityEmail": true, "visibilityContact": true,
		"searchAllowIndexing": false}`, Privacy(privacy))
}

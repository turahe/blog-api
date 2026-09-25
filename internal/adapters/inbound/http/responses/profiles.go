package responses

import (
	"time"

	"github.com/gin-gonic/gin"
	userdomain "github.com/turahe/blog-api/internal/core/user/domain"
)

// Profile renders the full profile for its owner.
func Profile(view userdomain.ProfileView) gin.H {
	user, profile, privacy := view.User, view.Profile, view.Privacy

	return gin.H{
		"id":                        user.UUID.String(),
		"username":                  user.Username,
		"email":                     user.Email,
		"emailVerifiedAt":           RFC3339(user.EmailVerifiedAt),
		"fullName":                  user.FullName,
		"displayName":               profile.DisplayName,
		"bio":                       profile.Bio,
		"avatarId":                  view.AvatarUUID,
		"contact":                   contact(profile),
		"socialLinks":               socialLinks(profile.Social),
		"locale":                    profile.Locale,
		"timezone":                  profile.Timezone,
		"marketingConsent":          profile.MarketingConsent,
		"marketingConsentUpdatedAt": RFC3339(profile.MarketingConsentUpdatedAt),
		"privacy":                   Privacy(privacy),
		"createdAt":                 user.CreatedAt.UTC().Format(time.RFC3339),
		"profileUpdatedAt":          RFC3339(profile.UpdatedAt),
	}
}

// AdminProfile is Profile plus account state for support staff.
func AdminProfile(view userdomain.ProfileView) gin.H {
	out := Profile(view)
	out["status"] = string(view.User.Status)
	out["lastLoginAt"] = RFC3339(view.User.LastLoginAt)
	out["loginCount"] = view.User.LoginCount

	return out
}

// PublicProfile renders what anyone may see, honouring the privacy settings.
func PublicProfile(view userdomain.ProfileView) gin.H {
	user, profile, privacy := view.User, view.Profile, view.Privacy

	out := gin.H{
		"id":          user.UUID.String(),
		"username":    user.Username,
		"fullName":    user.FullName,
		"displayName": profile.DisplayName,
		"bio":         profile.Bio,
		"avatarId":    view.AvatarUUID,
		"socialLinks": socialLinks(profile.Social),
		"joinedAt":    user.CreatedAt.UTC().Format(time.RFC3339),
	}
	if privacy.ShowContact {
		out["contact"] = contact(profile)
	}

	if privacy.ShowEmail {
		out["email"] = user.Email
	}

	return out
}

// Privacy renders a user's privacy settings.
func Privacy(privacy userdomain.Privacy) gin.H {
	return gin.H{
		"visibilityProfile":   string(privacy.Visibility),
		"visibilityEmail":     privacy.ShowEmail,
		"visibilityContact":   privacy.ShowContact,
		"searchAllowIndexing": privacy.AllowIndexing,
	}
}

func contact(profile userdomain.Profile) gin.H {
	return gin.H{"website": profile.ContactWebsite, "location": profile.ContactLocation}
}

func socialLinks(links userdomain.SocialLinks) gin.H {
	return gin.H{"twitter": links.Twitter, "linkedin": links.LinkedIn, "github": links.GitHub}
}

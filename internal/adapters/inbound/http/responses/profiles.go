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
		"id":                           user.UUID.String(),
		"username":                     user.Username,
		"email":                        user.Email,
		"email_verified_at":            RFC3339(user.EmailVerifiedAt),
		"full_name":                    user.FullName,
		"display_name":                 profile.DisplayName,
		"bio":                          profile.Bio,
		"avatar_id":                    view.AvatarUUID,
		"contact":                      contact(profile),
		"social_links":                 socialLinks(profile.Social),
		"locale":                       profile.Locale,
		"timezone":                     profile.Timezone,
		"marketing_consent":            profile.MarketingConsent,
		"marketing_consent_updated_at": RFC3339(profile.MarketingConsentUpdatedAt),
		"privacy":                      Privacy(privacy),
		"created_at":                   user.CreatedAt.UTC().Format(time.RFC3339),
		"profile_updated_at":           RFC3339(profile.UpdatedAt),
	}
}

// AdminProfile is Profile plus account state for support staff.
func AdminProfile(view userdomain.ProfileView) gin.H {
	out := Profile(view)
	out["status"] = string(view.User.Status)
	out["last_login_at"] = RFC3339(view.User.LastLoginAt)
	out["login_count"] = view.User.LoginCount

	return out
}

// PublicProfile renders what anyone may see, honouring the privacy settings.
func PublicProfile(view userdomain.ProfileView) gin.H {
	user, profile, privacy := view.User, view.Profile, view.Privacy

	out := gin.H{
		"id":           user.UUID.String(),
		"username":     user.Username,
		"full_name":    user.FullName,
		"display_name": profile.DisplayName,
		"bio":          profile.Bio,
		"avatar_id":    view.AvatarUUID,
		"social_links": socialLinks(profile.Social),
		"joined_at":    user.CreatedAt.UTC().Format(time.RFC3339),
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
		"visibility_profile":    string(privacy.Visibility),
		"visibility_email":      privacy.ShowEmail,
		"visibility_contact":    privacy.ShowContact,
		"search_allow_indexing": privacy.AllowIndexing,
	}
}

func contact(profile userdomain.Profile) gin.H {
	return gin.H{"website": profile.ContactWebsite, "location": profile.ContactLocation}
}

func socialLinks(links userdomain.SocialLinks) gin.H {
	return gin.H{"twitter": links.Twitter, "linkedin": links.LinkedIn, "github": links.GitHub}
}

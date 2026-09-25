package routes

import (
	nethttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
)

// Controllers groups Gin handlers by domain for Register*.
// A nil field is mounted as NotImplemented.
type Controllers struct {
	Stub func(Route) gin.HandlerFunc

	Health   Health
	Auth     Auth
	Users    Users
	Posts    Posts
	Cats     Categories
	Tags     Tags
	Media    Media
	Comments Comments
	Roles    Roles
	Activity Activity

	Notifications Notifications
}

// Notifications holds the caller's in-app inbox handlers.
type Notifications struct {
	List   gin.HandlerFunc
	Read   gin.HandlerFunc
	Stream gin.HandlerFunc
}

// Activity holds audit activity handlers.
type Activity struct {
	MeList        gin.HandlerFunc
	AdminUserList gin.HandlerFunc
}

// Roles holds role and permission administration handlers.
type Roles struct {
	List            gin.HandlerFunc
	Get             gin.HandlerFunc
	Create          gin.HandlerFunc
	Update          gin.HandlerFunc
	Delete          gin.HandlerFunc
	SetPermissions  gin.HandlerFunc
	Permissions     gin.HandlerFunc
	UserRolesList   gin.HandlerFunc
	UserRolesAssign gin.HandlerFunc
	UserRoleRevoke  gin.HandlerFunc
}

// Health probe handlers.
type Health struct {
	Live    gin.HandlerFunc
	Ready   gin.HandlerFunc
	Version gin.HandlerFunc
}

// Auth and password handlers.
type Auth struct {
	Login                 gin.HandlerFunc
	Refresh               gin.HandlerFunc
	Logout                gin.HandlerFunc
	PasswordForgot        gin.HandlerFunc
	PasswordResetValidity gin.HandlerFunc
	PasswordReset         gin.HandlerFunc
	MePasswordUpdate      gin.HandlerFunc

	AdminLogin             gin.HandlerFunc
	OAuthStart             gin.HandlerFunc
	OAuthCallback          gin.HandlerFunc
	TwoFactorChallenge     gin.HandlerFunc
	MeTwoFactorGet         gin.HandlerFunc
	MeTwoFactorSetup       gin.HandlerFunc
	MeTwoFactorConfirm     gin.HandlerFunc
	MeTwoFactorDisable     gin.HandlerFunc
	MeTwoFactorBackupCodes gin.HandlerFunc
}

// Users self-service and admin handlers.
type Users struct {
	MeGet                gin.HandlerFunc
	MeProfilePatch       gin.HandlerFunc
	MeAvatarUpload       gin.HandlerFunc
	MeAvatarDelete       gin.HandlerFunc
	MeEmailRequestChange gin.HandlerFunc
	MeEmailConfirmChange gin.HandlerFunc
	PublicProfile        gin.HandlerFunc
	AdminUsersList       gin.HandlerFunc
	AdminCreate          gin.HandlerFunc
	AdminPasswordReset   gin.HandlerFunc
	AdminProfileGet      gin.HandlerFunc
	AdminProfilePatch    gin.HandlerFunc
}

// Posts public and admin handlers.
type Posts struct {
	PublicList        gin.HandlerFunc
	PublicGet         gin.HandlerFunc
	AdminList         gin.HandlerFunc
	AdminCreate       gin.HandlerFunc
	AdminPublish      gin.HandlerFunc
	AdminUnpublish    gin.HandlerFunc
	AdminArchive      gin.HandlerFunc
	AdminUpdate       gin.HandlerFunc
	AdminMediaReplace gin.HandlerFunc
	AdminDelete       gin.HandlerFunc
	AdminRestore      gin.HandlerFunc
}

// Categories public and admin handlers.
type Categories struct {
	PublicList  gin.HandlerFunc
	PublicGet   gin.HandlerFunc
	AdminList   gin.HandlerFunc
	AdminCreate gin.HandlerFunc
	AdminUpdate gin.HandlerFunc
	AdminDelete gin.HandlerFunc
	AdminMove   gin.HandlerFunc
}

// Tags public and admin handlers.
type Tags struct {
	PublicList  gin.HandlerFunc
	AdminCreate gin.HandlerFunc
	AdminUpdate gin.HandlerFunc
	AdminMerge  gin.HandlerFunc
	AdminDelete gin.HandlerFunc
}

// Media public and admin handlers.
type Media struct {
	PublicGet      gin.HandlerFunc
	AdminCreate    gin.HandlerFunc
	AdminComplete  gin.HandlerFunc
	AdminList      gin.HandlerFunc
	AdminTagsPatch gin.HandlerFunc
	AdminDelete    gin.HandlerFunc
}

// Comments public, self-service, and moderation handlers.
type Comments struct {
	PostList          gin.HandlerFunc
	PostCreate        gin.HandlerFunc
	Get               gin.HandlerFunc
	Flag              gin.HandlerFunc
	MeList            gin.HandlerFunc
	Patch             gin.HandlerFunc
	Delete            gin.HandlerFunc
	Upvote            gin.HandlerFunc
	AdminList         gin.HandlerFunc
	AdminGet          gin.HandlerFunc
	AdminStats        gin.HandlerFunc
	AdminModerate     gin.HandlerFunc
	AdminBulkModerate gin.HandlerFunc
	AdminHardDelete   gin.HandlerFunc
}

// NotImplemented is the fallback for registered but unwired operations.
func NotImplemented(route Route) gin.HandlerFunc {
	return func(c *gin.Context) {
		responses.FailureWithDetails(c, nethttp.StatusNotImplemented, "operation.not_implemented",
			"Operation is registered but not implemented",
			gin.H{"operation_id": route.OperationID, "route_group": route.Group, "auth_mode": route.Auth})
	}
}

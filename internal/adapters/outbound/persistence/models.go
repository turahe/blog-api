package persistence

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserModel struct {
	ID                uuid.UUID `gorm:"type:uuid;primaryKey"`
	Email             string    `gorm:"column:email"`
	Username          string    `gorm:"column:username"`
	FullName          string    `gorm:"column:full_name"`
	PasswordHash      *string   `gorm:"column:password_hash"`
	Status            string    `gorm:"column:status"`
	EmailVerifiedAt   *time.Time
	PasswordChangedAt *time.Time
	LastLoginAt       *time.Time
	LoginCount        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         gorm.DeletedAt `gorm:"index"`
}

func (UserModel) TableName() string { return "users" }

type RefreshSessionModel struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID  `gorm:"type:uuid;column:user_id"`
	FamilyID   uuid.UUID  `gorm:"type:uuid;column:family_id"`
	TokenHash  string     `gorm:"column:token_hash"`
	ExpiresAt  time.Time  `gorm:"column:expires_at"`
	RevokedAt  *time.Time `gorm:"column:revoked_at"`
	ReplacedBy *uuid.UUID `gorm:"type:uuid;column:replaced_by"`
	UserAgent  *string    `gorm:"column:user_agent"`
	IPAddress  *string    `gorm:"column:ip_address"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
}

func (RefreshSessionModel) TableName() string { return "refresh_sessions" }

type PostModel struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`
	AuthorID    uuid.UUID  `gorm:"type:uuid;column:author_id"`
	CategoryID  *uuid.UUID `gorm:"type:uuid;column:category_id"`
	Title       string
	Slug        string
	Excerpt     *string
	Content     string
	Status      string
	Version     int64
	PublishedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

func (PostModel) TableName() string { return "posts" }

type RoleModel struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (RoleModel) TableName() string { return "roles" }

type PermissionModel struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Key         string
	Description *string
	CreatedAt   time.Time
}

func (PermissionModel) TableName() string { return "permissions" }

type UserRoleModel struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey;column:user_id"`
	RoleID    uuid.UUID `gorm:"type:uuid;primaryKey;column:role_id"`
	CreatedAt time.Time
}

func (UserRoleModel) TableName() string { return "user_roles" }

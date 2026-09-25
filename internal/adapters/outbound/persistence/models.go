package persistence

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// UserModel is the users row.
type UserModel struct {
	ID                int64     `gorm:"primaryKey"`
	UUID              uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
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

// TableName returns the users table name for GORM.
func (UserModel) TableName() string { return "users" }

// RefreshSessionModel is a refresh_sessions row; rotated sessions link to their replacement.
type RefreshSessionModel struct {
	ID             int64      `gorm:"primaryKey"`
	UUID           uuid.UUID  `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	UserID         int64      `gorm:"column:user_id"`
	FamilyID       uuid.UUID  `gorm:"type:uuid;column:family_id"`
	TokenHash      string     `gorm:"column:token_hash"`
	ExpiresAt      time.Time  `gorm:"column:expires_at"`
	RevokedAt      *time.Time `gorm:"column:revoked_at"`
	ReplacedBy     *int64     `gorm:"column:replaced_by"`
	UserAgent      *string    `gorm:"column:user_agent"`
	IPAddress      *string    `gorm:"column:ip_address"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UserUUID       uuid.UUID  `gorm:"column:user_uuid;->"`
	ReplacedByUUID *uuid.UUID `gorm:"column:replaced_by_uuid;->"`
}

// TableName returns the refresh_sessions table name for GORM.
func (RefreshSessionModel) TableName() string { return "refresh_sessions" }

// PostModel is the posts row.
type PostModel struct {
	ID                  int64     `gorm:"primaryKey"`
	UUID                uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	AuthorID            int64     `gorm:"column:author_id"`
	CategoryID          *int64    `gorm:"column:category_id"`
	Title               string
	Slug                string
	Excerpt             *string
	Content             string
	CoverImageMediaID   *int64 `gorm:"column:cover_image_media_id"`
	Status              string
	CommentPolicy       string `gorm:"column:comment_policy;default:open"`
	Version             int64
	PublishedAt         *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           gorm.DeletedAt `gorm:"index"`
	AuthorUUID          uuid.UUID      `gorm:"column:author_uuid;->"`
	CategoryUUID        *uuid.UUID     `gorm:"column:category_uuid;->"`
	CoverImageMediaUUID *uuid.UUID     `gorm:"column:cover_image_media_uuid;->"`
}

// TableName returns the posts table name for GORM.
func (PostModel) TableName() string { return "posts" }

// MediaAssetModel is the media_assets row.
type MediaAssetModel struct {
	ID               int64          `gorm:"primaryKey"`
	UUID             uuid.UUID      `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	StorageKey       string         `gorm:"column:storage_key"`
	OriginalFilename string         `gorm:"column:original_filename"`
	ContentType      string         `gorm:"column:content_type"`
	SizeBytes        int64          `gorm:"column:size_bytes"`
	Width            *int           `gorm:"column:width"`
	Height           *int           `gorm:"column:height"`
	ChecksumSHA256   *string        `gorm:"column:checksum_sha256"`
	Disk             string         `gorm:"column:disk"`
	Status           string         `gorm:"column:status"`
	UploadedBy       *int64         `gorm:"column:uploaded_by"`
	Tags             pq.StringArray `gorm:"type:text[];column:tags"`
	PresignExpiresAt *time.Time     `gorm:"column:presign_expires_at"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at;index"`
	UploadedByUUID   *uuid.UUID     `gorm:"column:uploaded_by_uuid;->"`
}

// TableName returns the media_assets table name for GORM.
func (MediaAssetModel) TableName() string { return "media_assets" }

// RoleModel is the roles row.
type RoleModel struct {
	ID          int64     `gorm:"primaryKey"`
	UUID        uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	Name        string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName returns the roles table name for GORM.
func (RoleModel) TableName() string { return "roles" }

// PermissionModel is the permissions row.
type PermissionModel struct {
	ID          int64     `gorm:"primaryKey"`
	UUID        uuid.UUID `gorm:"type:uuid;column:uuid;default:gen_random_uuid()"`
	Key         string
	Description *string
	CreatedAt   time.Time
}

// TableName returns the permissions table name for GORM.
func (PermissionModel) TableName() string { return "permissions" }

// UserRoleModel is the user_roles join row.
type UserRoleModel struct {
	UserID    int64 `gorm:"primaryKey;autoIncrement:false;column:user_id"`
	RoleID    int64 `gorm:"primaryKey;autoIncrement:false;column:role_id"`
	CreatedAt time.Time
}

// TableName returns the user_roles table name for GORM.
func (UserRoleModel) TableName() string { return "user_roles" }

// RolePermissionModel is the role_permissions join row.
type RolePermissionModel struct {
	RoleID       int64 `gorm:"primaryKey;autoIncrement:false;column:role_id"`
	PermissionID int64 `gorm:"primaryKey;autoIncrement:false;column:permission_id"`
	CreatedAt    time.Time
}

// TableName returns the role_permissions table name for GORM.
func (RolePermissionModel) TableName() string { return "role_permissions" }

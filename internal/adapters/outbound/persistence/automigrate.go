package persistence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"

	"gorm.io/gorm"
)

// migrateMu serializes schema changes on one process.
var migrateMu sync.Mutex

// MigrateModels returns every GORM model AutoMigrate must create or update.
func MigrateModels() []any {
	models := []any{
		&UserModel{},
		&RefreshSessionModel{},
		&RoleModel{},
		&PermissionModel{},
		&UserRoleModel{},
		&RolePermissionModel{},
		&CategoryModel{},
		&TagModel{},
		&PostModel{},
		&PostTagModel{},
		&MediaAssetModel{},
		&PostMediaModel{},
		&CommentModel{},
		&ModerationLogModel{},
		&PasswordResetTokenModel{},
		&TwoFactorMethodModel{},
		&TwoFactorBackupCodeModel{},
		&OAuthIdentityModel{},
		&NotificationTemplateModel{},
		&AuditLogModel{},
		&RegistrationModel{},
	}

	models = append(models,
		&CasbinRulesSchema{},
		&CommentFlagsSchema{},
		&CommentUpvotesSchema{},
		&UserProfilesSchema{},
		&UserPrivacySettingsSchema{},
		&NotificationsSchema{},
		&OutboxEventsSchema{},
		&ProcessedMessagesSchema{},
		&ScheduledJobRunsSchema{},
		&SettingsSchema{},
		&SettingsHistorySchema{},
		&PostRevisionsSchema{},
		&PostSeoSchema{},
		&ConsentSubjectsSchema{},
		&AnalyticsConsentsSchema{},
		&PrivacyRequestsSchema{},
		&ImpersonationSessionsSchema{},
		&NewsletterListsSchema{},
		&NewsletterProviderConfigSchema{},
		&NewsletterSubscribersSchema{},
		&NewsletterListMembershipsSchema{},
		&NewsletterTokensSchema{},
		&NewsletterConsentAuditSchema{},
		&NewsletterIssuesSchema{},
		&NewsletterIssueListsSchema{},
		&NewsletterDeliveriesSchema{},
		&AnalyticsPageViewsSchema{},
		&AnalyticsNavigationSchema{},
		&AnalyticsSearchesSchema{},
		&AnalyticsSearchClicksSchema{},
		&AnalyticsTimeSpentSchema{},
		&AnalyticsSubjectFirstSeenSchema{},
		&AnalyticsRollupSiteSchema{},
		&AnalyticsRollupPagesSchema{},
		&AnalyticsRollupReferrersSchema{},
		&AnalyticsRollupSearchesSchema{},
		&AnalyticsRollupSearchResultsSchema{},
		&AnalyticsRollupSearchPositionsSchema{},
		&AnalyticsRollupNavigationSchema{},
		&AnalyticsRollupCohortsSchema{},
		&AnalyticsRollupDimensionsSchema{},
		&AnalyticsRollupStateSchema{},
		&AnalyticsExportsSchema{},
		&AnalyticsSaltsSchema{},
	)

	return models
}

// AutoMigrate creates or updates tables for MigrateModels.
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("automigrate: nil db")
	}

	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		return fmt.Errorf("ensure pgcrypto: %w", err)
	}

	if err := db.AutoMigrate(MigrateModels()...); err != nil {
		return fmt.Errorf("gorm automigrate: %w", err)
	}

	return nil
}

// Migrate applies AutoMigrate and ensures posts.search_vector (generated column + GIN index).
func Migrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("migrate: nil db")
	}

	migrateMu.Lock()
	defer migrateMu.Unlock()

	session := db.Session(&gorm.Session{})
	session.DisableForeignKeyConstraintWhenMigrating = true

	if err := AutoMigrate(session); err != nil {
		return err
	}

	if err := RebuildPostSearchIndex(context.Background(), session, "simple"); err != nil {
		return fmt.Errorf("ensure post search index: %w", err)
	}

	return nil
}

// DropAll drops every MigrateModels table (destructive).
func DropAll(db *gorm.DB) error {
	if db == nil {
		return errors.New("drop all: nil db")
	}

	migrateMu.Lock()
	defer migrateMu.Unlock()

	models := MigrateModels()
	for _, model := range slices.Backward(models) {
		if err := db.Migrator().DropTable(model); err != nil {
			return fmt.Errorf("drop table: %w", err)
		}
	}

	return nil
}

// SchemaStatus writes present/missing for each migrate model to stdout.
func SchemaStatus(db *gorm.DB) error {
	return SchemaStatusTo(db, os.Stdout)
}

// SchemaStatusTo writes present/missing lines to w.
func SchemaStatusTo(db *gorm.DB, w io.Writer) error {
	if db == nil {
		return errors.New("schema status: nil db")
	}

	migrateMu.Lock()
	defer migrateMu.Unlock()

	for _, model := range MigrateModels() {
		name := schemaTableName(db, model)

		state := "missing"
		if db.Migrator().HasTable(model) {
			state = "present"
		}

		if _, err := fmt.Fprintf(w, "%s\t%s\n", state, name); err != nil {
			return err
		}
	}

	return nil
}

func schemaTableName(db *gorm.DB, model any) string {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return fmt.Sprintf("%T", model)
	}

	return stmt.Schema.Table
}

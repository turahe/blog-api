package persistence

// Schema models used only by AutoMigrate. Prefer repository *Model types when
// they already exist; these cover tables that were historically Raw-SQL-only.

import (
	"time"

	"github.com/google/uuid"
)

// AnalyticsConsentsSchema maps analytics_consents for AutoMigrate.
type AnalyticsConsentsSchema struct {
	Id            int64      `gorm:"primaryKey;column:id"`
	Uuid          uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	SubjectId     int64      `gorm:"column:subject_id"`
	Purpose       string     `gorm:"column:purpose"`
	Status        string     `gorm:"column:status"`
	PolicyVersion string     `gorm:"column:policy_version"`
	DecidedAt     time.Time  `gorm:"column:decided_at"`
	WithdrawnAt   *time.Time `gorm:"column:withdrawn_at"`
}

func (AnalyticsConsentsSchema) TableName() string { return "analytics_consents" }

// AnalyticsExportsSchema maps analytics_exports for AutoMigrate.
type AnalyticsExportsSchema struct {
	Id          int64      `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	UserId      int64      `gorm:"column:user_id"`
	Grain       string     `gorm:"column:grain"`
	FirstDay    time.Time  `gorm:"column:first_day"`
	LastDay     time.Time  `gorm:"column:last_day"`
	Timezone    string     `gorm:"column:timezone"`
	Status      string     `gorm:"column:status"`
	StorageKey  *string    `gorm:"column:storage_key"`
	SizeBytes   *int64     `gorm:"column:size_bytes"`
	ExpiresAt   *time.Time `gorm:"column:expires_at"`
	Attempts    int        `gorm:"column:attempts"`
	LastError   *string    `gorm:"column:last_error"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

func (AnalyticsExportsSchema) TableName() string { return "analytics_exports" }

// AnalyticsNavigationSchema maps analytics_navigation for AutoMigrate.
type AnalyticsNavigationSchema struct {
	Id             int64      `gorm:"primaryKey;column:id"`
	Uuid           uuid.UUID  `gorm:"column:uuid;type:uuid"`
	SubjectUuid    *uuid.UUID `gorm:"column:subject_uuid;type:uuid"`
	VisitorHash    string     `gorm:"column:visitor_hash"`
	SessionId      uuid.UUID  `gorm:"column:session_id;type:uuid"`
	FromPath       *string    `gorm:"column:from_path"`
	ToPath         string     `gorm:"column:to_path"`
	TransitionType string     `gorm:"column:transition_type"`
	OccurredAt     time.Time  `gorm:"column:occurred_at"`
}

func (AnalyticsNavigationSchema) TableName() string { return "analytics_navigation" }

// AnalyticsPageViewsSchema maps analytics_page_views for AutoMigrate.
type AnalyticsPageViewsSchema struct {
	Id          int64      `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID  `gorm:"column:uuid;type:uuid"`
	SubjectUuid *uuid.UUID `gorm:"column:subject_uuid;type:uuid"`
	VisitorHash string     `gorm:"column:visitor_hash"`
	SessionId   uuid.UUID  `gorm:"column:session_id;type:uuid"`
	Path        string     `gorm:"column:path"`
	Referrer    *string    `gorm:"column:referrer"`
	CountryCode *string    `gorm:"column:country_code"`
	DeviceType  string     `gorm:"column:device_type"`
	Browser     string     `gorm:"column:browser"`
	OccurredAt  time.Time  `gorm:"column:occurred_at"`
}

func (AnalyticsPageViewsSchema) TableName() string { return "analytics_page_views" }

// AnalyticsRollupCohortsSchema maps analytics_rollup_cohorts for AutoMigrate.
type AnalyticsRollupCohortsSchema struct {
	CohortDay  time.Time `gorm:"primaryKey;column:cohort_day"`
	Size       int64     `gorm:"column:size"`
	Day1       *int64    `gorm:"column:day1"`
	Day7       *int64    `gorm:"column:day7"`
	Day30      *int64    `gorm:"column:day30"`
	ComputedAt time.Time `gorm:"column:computed_at"`
}

func (AnalyticsRollupCohortsSchema) TableName() string { return "analytics_rollup_cohorts" }

// AnalyticsRollupDimensionsSchema maps analytics_rollup_dimensions for AutoMigrate.
type AnalyticsRollupDimensionsSchema struct {
	Grain       string    `gorm:"primaryKey;column:grain"`
	PeriodStart time.Time `gorm:"primaryKey;column:period_start"`
	Dimension   string    `gorm:"primaryKey;column:dimension"`
	Value       string    `gorm:"primaryKey;column:value"`
	Views       int64     `gorm:"column:views"`
	Visitors    int64     `gorm:"column:visitors"`
}

func (AnalyticsRollupDimensionsSchema) TableName() string { return "analytics_rollup_dimensions" }

// AnalyticsRollupNavigationSchema maps analytics_rollup_navigation for AutoMigrate.
type AnalyticsRollupNavigationSchema struct {
	Grain       string    `gorm:"primaryKey;column:grain"`
	PeriodStart time.Time `gorm:"primaryKey;column:period_start"`
	FromPath    string    `gorm:"primaryKey;column:from_path"`
	ToPath      string    `gorm:"primaryKey;column:to_path"`
	Transition  string    `gorm:"primaryKey;column:transition"`
	Transitions int64     `gorm:"column:transitions"`
}

func (AnalyticsRollupNavigationSchema) TableName() string { return "analytics_rollup_navigation" }

// AnalyticsRollupPagesSchema maps analytics_rollup_pages for AutoMigrate.
type AnalyticsRollupPagesSchema struct {
	Grain        string    `gorm:"primaryKey;column:grain"`
	PeriodStart  time.Time `gorm:"primaryKey;column:period_start"`
	Path         string    `gorm:"primaryKey;column:path"`
	Views        int64     `gorm:"column:views"`
	Visitors     int64     `gorm:"column:visitors"`
	Entries      int64     `gorm:"column:entries"`
	Exits        int64     `gorm:"column:exits"`
	FocusSeconds int64     `gorm:"column:focus_seconds"`
	FocusViews   int64     `gorm:"column:focus_views"`
}

func (AnalyticsRollupPagesSchema) TableName() string { return "analytics_rollup_pages" }

// AnalyticsRollupReferrersSchema maps analytics_rollup_referrers for AutoMigrate.
type AnalyticsRollupReferrersSchema struct {
	Grain       string    `gorm:"primaryKey;column:grain"`
	PeriodStart time.Time `gorm:"primaryKey;column:period_start"`
	Host        string    `gorm:"primaryKey;column:host"`
	Sessions    int64     `gorm:"column:sessions"`
	Visitors    int64     `gorm:"column:visitors"`
}

func (AnalyticsRollupReferrersSchema) TableName() string { return "analytics_rollup_referrers" }

// AnalyticsRollupSearchPositionsSchema maps analytics_rollup_search_positions for AutoMigrate.
type AnalyticsRollupSearchPositionsSchema struct {
	Grain       string    `gorm:"primaryKey;column:grain"`
	PeriodStart time.Time `gorm:"primaryKey;column:period_start"`
	Position    int       `gorm:"primaryKey;column:position"`
	Clicks      int64     `gorm:"column:clicks"`
}

func (AnalyticsRollupSearchPositionsSchema) TableName() string {
	return "analytics_rollup_search_positions"
}

// AnalyticsRollupSearchResultsSchema maps analytics_rollup_search_results for AutoMigrate.
type AnalyticsRollupSearchResultsSchema struct {
	Grain        string    `gorm:"primaryKey;column:grain"`
	PeriodStart  time.Time `gorm:"primaryKey;column:period_start"`
	Query        string    `gorm:"primaryKey;column:query"`
	ResourceType string    `gorm:"primaryKey;column:resource_type"`
	ResourceUuid uuid.UUID `gorm:"primaryKey;column:resource_uuid;type:uuid"`
	Clicks       int64     `gorm:"column:clicks"`
}

func (AnalyticsRollupSearchResultsSchema) TableName() string {
	return "analytics_rollup_search_results"
}

// AnalyticsRollupSearchesSchema maps analytics_rollup_searches for AutoMigrate.
type AnalyticsRollupSearchesSchema struct {
	Grain             string    `gorm:"primaryKey;column:grain"`
	PeriodStart       time.Time `gorm:"primaryKey;column:period_start"`
	Query             string    `gorm:"primaryKey;column:query"`
	Searches          int64     `gorm:"column:searches"`
	Visitors          int64     `gorm:"column:visitors"`
	ZeroResults       int64     `gorm:"column:zero_results"`
	SearchesWithClick int64     `gorm:"column:searches_with_click"`
	Clicks            int64     `gorm:"column:clicks"`
	ClickSeconds      int64     `gorm:"column:click_seconds"`
}

func (AnalyticsRollupSearchesSchema) TableName() string { return "analytics_rollup_searches" }

// AnalyticsRollupSiteSchema maps analytics_rollup_site for AutoMigrate.
type AnalyticsRollupSiteSchema struct {
	Grain              string    `gorm:"primaryKey;column:grain"`
	PeriodStart        time.Time `gorm:"primaryKey;column:period_start"`
	Views              int64     `gorm:"column:views"`
	Visitors           int64     `gorm:"column:visitors"`
	Sessions           int64     `gorm:"column:sessions"`
	Bounces            int64     `gorm:"column:bounces"`
	FocusSeconds       int64     `gorm:"column:focus_seconds"`
	FocusViews         int64     `gorm:"column:focus_views"`
	Searches           int64     `gorm:"column:searches"`
	ZeroResultSearches int64     `gorm:"column:zero_result_searches"`
	SearchesWithClick  int64     `gorm:"column:searches_with_click"`
	SearchClicks       int64     `gorm:"column:search_clicks"`
	ConsentedVisitors  int64     `gorm:"column:consented_visitors"`
	NewVisitors        int64     `gorm:"column:new_visitors"`
	ComputedAt         time.Time `gorm:"column:computed_at"`
}

func (AnalyticsRollupSiteSchema) TableName() string { return "analytics_rollup_site" }

// AnalyticsRollupStateSchema maps analytics_rollup_state for AutoMigrate.
type AnalyticsRollupStateSchema struct {
	Id        int64     `gorm:"primaryKey;column:id"`
	Timezone  string    `gorm:"column:timezone"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (AnalyticsRollupStateSchema) TableName() string { return "analytics_rollup_state" }

// AnalyticsSaltsSchema maps analytics_salts for AutoMigrate.
type AnalyticsSaltsSchema struct {
	Day       time.Time `gorm:"primaryKey;column:day"`
	Salt      []byte    `gorm:"column:salt"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (AnalyticsSaltsSchema) TableName() string { return "analytics_salts" }

// AnalyticsSearchClicksSchema maps analytics_search_clicks for AutoMigrate.
type AnalyticsSearchClicksSchema struct {
	Id           int64      `gorm:"primaryKey;column:id"`
	Uuid         uuid.UUID  `gorm:"column:uuid;type:uuid"`
	SearchUuid   uuid.UUID  `gorm:"column:search_uuid;type:uuid"`
	SubjectUuid  *uuid.UUID `gorm:"column:subject_uuid;type:uuid"`
	VisitorHash  string     `gorm:"column:visitor_hash"`
	SessionId    uuid.UUID  `gorm:"column:session_id;type:uuid"`
	Position     int        `gorm:"column:position"`
	ResourceType string     `gorm:"column:resource_type"`
	ResourceUuid uuid.UUID  `gorm:"column:resource_uuid;type:uuid"`
	OccurredAt   time.Time  `gorm:"column:occurred_at"`
}

func (AnalyticsSearchClicksSchema) TableName() string { return "analytics_search_clicks" }

// AnalyticsSearchesSchema maps analytics_searches for AutoMigrate.
type AnalyticsSearchesSchema struct {
	Id          int64      `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID  `gorm:"column:uuid;type:uuid"`
	SubjectUuid *uuid.UUID `gorm:"column:subject_uuid;type:uuid"`
	VisitorHash string     `gorm:"column:visitor_hash"`
	SessionId   uuid.UUID  `gorm:"column:session_id;type:uuid"`
	Query       string     `gorm:"column:query"`
	ResultCount int        `gorm:"column:result_count"`
	Filters     []byte     `gorm:"column:filters;type:jsonb"`
	OccurredAt  time.Time  `gorm:"column:occurred_at"`
}

func (AnalyticsSearchesSchema) TableName() string { return "analytics_searches" }

// AnalyticsSubjectFirstSeenSchema maps analytics_subject_first_seen for AutoMigrate.
type AnalyticsSubjectFirstSeenSchema struct {
	SubjectUuid uuid.UUID `gorm:"primaryKey;column:subject_uuid;type:uuid"`
	FirstSeenAt time.Time `gorm:"column:first_seen_at"`
}

func (AnalyticsSubjectFirstSeenSchema) TableName() string { return "analytics_subject_first_seen" }

// AnalyticsTimeSpentSchema maps analytics_time_spent for AutoMigrate.
type AnalyticsTimeSpentSchema struct {
	Id           int64      `gorm:"primaryKey;column:id"`
	Uuid         uuid.UUID  `gorm:"column:uuid;type:uuid"`
	SubjectUuid  *uuid.UUID `gorm:"column:subject_uuid;type:uuid"`
	VisitorHash  string     `gorm:"column:visitor_hash"`
	SessionId    uuid.UUID  `gorm:"column:session_id;type:uuid"`
	Path         string     `gorm:"column:path"`
	FocusSeconds int        `gorm:"column:focus_seconds"`
	StartedAt    time.Time  `gorm:"column:started_at"`
	LastSeenAt   time.Time  `gorm:"column:last_seen_at"`
}

func (AnalyticsTimeSpentSchema) TableName() string { return "analytics_time_spent" }

// CasbinRulesSchema maps casbin_rules for AutoMigrate.
type CasbinRulesSchema struct {
	Id    int64     `gorm:"primaryKey;column:id"`
	Uuid  uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	Ptype *string   `gorm:"column:ptype"`
	V0    *string   `gorm:"column:v0"`
	V1    *string   `gorm:"column:v1"`
	V2    *string   `gorm:"column:v2"`
	V3    *string   `gorm:"column:v3"`
	V4    *string   `gorm:"column:v4"`
	V5    *string   `gorm:"column:v5"`
}

func (CasbinRulesSchema) TableName() string { return "casbin_rules" }

// CommentFlagsSchema maps comment_flags for AutoMigrate.
type CommentFlagsSchema struct {
	Id             int64     `gorm:"primaryKey;column:id"`
	Uuid           uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	CommentId      int64     `gorm:"column:comment_id"`
	ReporterUserId *int64    `gorm:"column:reporter_user_id"`
	ReporterIpHash *string   `gorm:"column:reporter_ip_hash"`
	ReasonCode     string    `gorm:"column:reason_code"`
	Details        *string   `gorm:"column:details"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (CommentFlagsSchema) TableName() string { return "comment_flags" }

// CommentUpvotesSchema maps comment_upvotes for AutoMigrate.
type CommentUpvotesSchema struct {
	Id          int64     `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	CommentId   int64     `gorm:"column:comment_id"`
	VoterUserId int64     `gorm:"column:voter_user_id"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

func (CommentUpvotesSchema) TableName() string { return "comment_upvotes" }

// ConsentSubjectsSchema maps consent_subjects for AutoMigrate.
type ConsentSubjectsSchema struct {
	Id         int64     `gorm:"primaryKey;column:id"`
	Uuid       uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	TokenHash  string    `gorm:"column:token_hash"`
	UserId     *int64    `gorm:"column:user_id"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	LastSeenAt time.Time `gorm:"column:last_seen_at"`
}

func (ConsentSubjectsSchema) TableName() string { return "consent_subjects" }

// ImpersonationSessionsSchema maps impersonation_sessions for AutoMigrate.
type ImpersonationSessionsSchema struct {
	Id           int64      `gorm:"primaryKey;column:id"`
	Uuid         uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	ActorId      int64      `gorm:"column:actor_id"`
	TargetId     int64      `gorm:"column:target_id"`
	State        string     `gorm:"column:state"`
	Reason       string     `gorm:"column:reason"`
	IpAddress    *string    `gorm:"column:ip_address"`
	UserAgent    *string    `gorm:"column:user_agent"`
	StartedAt    time.Time  `gorm:"column:started_at"`
	ExpiresAt    time.Time  `gorm:"column:expires_at"`
	EndedAt      *time.Time `gorm:"column:ended_at"`
	EndReason    *string    `gorm:"column:end_reason"`
	BaseFamilyId *uuid.UUID `gorm:"column:base_family_id;type:uuid"`
}

func (ImpersonationSessionsSchema) TableName() string { return "impersonation_sessions" }

// NewsletterConsentAuditSchema maps newsletter_consent_audit for AutoMigrate.
type NewsletterConsentAuditSchema struct {
	Id           int64     `gorm:"primaryKey;column:id"`
	SubscriberId int64     `gorm:"column:subscriber_id"`
	Event        string    `gorm:"column:event"`
	ListSlug     *string   `gorm:"column:list_slug"`
	Source       string    `gorm:"column:source"`
	ReasonCode   *string   `gorm:"column:reason_code"`
	Feedback     *string   `gorm:"column:feedback"`
	IpHash       *string   `gorm:"column:ip_hash"`
	OccurredAt   time.Time `gorm:"column:occurred_at"`
}

func (NewsletterConsentAuditSchema) TableName() string { return "newsletter_consent_audit" }

// NewsletterDeliveriesSchema maps newsletter_deliveries for AutoMigrate.
type NewsletterDeliveriesSchema struct {
	Id           int64      `gorm:"primaryKey;column:id"`
	Uuid         uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	IssueId      int64      `gorm:"column:issue_id"`
	SubscriberId int64      `gorm:"column:subscriber_id"`
	Status       string     `gorm:"column:status"`
	Attempts     int        `gorm:"column:attempts"`
	Permanent    bool       `gorm:"column:permanent"`
	LastError    *string    `gorm:"column:last_error"`
	SentAt       *time.Time `gorm:"column:sent_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

func (NewsletterDeliveriesSchema) TableName() string { return "newsletter_deliveries" }

// NewsletterIssueListsSchema maps newsletter_issue_lists for AutoMigrate.
type NewsletterIssueListsSchema struct {
	IssueId int64 `gorm:"primaryKey;column:issue_id"`
	ListId  int64 `gorm:"primaryKey;column:list_id"`
}

func (NewsletterIssueListsSchema) TableName() string { return "newsletter_issue_lists" }

// NewsletterIssuesSchema maps newsletter_issues for AutoMigrate.
type NewsletterIssuesSchema struct {
	Id           int64      `gorm:"primaryKey;column:id"`
	Uuid         uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	Subject      string     `gorm:"column:subject"`
	Preheader    string     `gorm:"column:preheader"`
	BodyMarkdown string     `gorm:"column:body_markdown"`
	Status       string     `gorm:"column:status"`
	SendAt       *time.Time `gorm:"column:send_at"`
	QueuedAt     *time.Time `gorm:"column:queued_at"`
	StartedAt    *time.Time `gorm:"column:started_at"`
	CompletedAt  *time.Time `gorm:"column:completed_at"`
	SentCount    int        `gorm:"column:sent_count"`
	FailedCount  int        `gorm:"column:failed_count"`
	CreatedBy    *int64     `gorm:"column:created_by"`
	UpdatedBy    *int64     `gorm:"column:updated_by"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

func (NewsletterIssuesSchema) TableName() string { return "newsletter_issues" }

// NewsletterListMembershipsSchema maps newsletter_list_memberships for AutoMigrate.
type NewsletterListMembershipsSchema struct {
	SubscriberId int64      `gorm:"primaryKey;column:subscriber_id"`
	ListId       int64      `gorm:"primaryKey;column:list_id"`
	State        string     `gorm:"column:state"`
	JoinedAt     *time.Time `gorm:"column:joined_at"`
	LeftAt       *time.Time `gorm:"column:left_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

func (NewsletterListMembershipsSchema) TableName() string { return "newsletter_list_memberships" }

// NewsletterListsSchema maps newsletter_lists for AutoMigrate.
type NewsletterListsSchema struct {
	Id          int64      `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	Slug        string     `gorm:"column:slug"`
	Name        string     `gorm:"column:name"`
	Description string     `gorm:"column:description"`
	IsDefault   bool       `gorm:"column:is_default"`
	Position    int        `gorm:"column:position"`
	ArchivedAt  *time.Time `gorm:"column:archived_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
}

func (NewsletterListsSchema) TableName() string { return "newsletter_lists" }

// NewsletterProviderConfigSchema maps newsletter_provider_config for AutoMigrate.
type NewsletterProviderConfigSchema struct {
	Id                  int64     `gorm:"primaryKey;column:id"`
	FromName            string    `gorm:"column:from_name"`
	FromEmail           string    `gorm:"column:from_email"`
	ReplyTo             string    `gorm:"column:reply_to"`
	PostalAddress       string    `gorm:"column:postal_address"`
	ConfirmTtlSeconds   int       `gorm:"column:confirm_ttl_seconds"`
	DoubleOptinRequired bool      `gorm:"column:double_optin_required"`
	UpdatedBy           *int64    `gorm:"column:updated_by"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
}

func (NewsletterProviderConfigSchema) TableName() string { return "newsletter_provider_config" }

// NewsletterSubscribersSchema maps newsletter_subscribers for AutoMigrate.
type NewsletterSubscribersSchema struct {
	Id                     int64      `gorm:"primaryKey;column:id"`
	Uuid                   uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	Email                  *string    `gorm:"column:email"`
	NormalizedEmail        *string    `gorm:"column:normalized_email"`
	DisplayName            *string    `gorm:"column:display_name"`
	UserId                 *int64     `gorm:"column:user_id"`
	Status                 string     `gorm:"column:status"`
	Format                 string     `gorm:"column:format"`
	Source                 string     `gorm:"column:source"`
	IpHash                 *string    `gorm:"column:ip_hash"`
	UserAgent              *string    `gorm:"column:user_agent"`
	ConfirmSends           int        `gorm:"column:confirm_sends"`
	ConfirmWindowStartedAt *time.Time `gorm:"column:confirm_window_started_at"`
	OptedInAt              *time.Time `gorm:"column:opted_in_at"`
	UnsubscribedAt         *time.Time `gorm:"column:unsubscribed_at"`
	BouncedAt              *time.Time `gorm:"column:bounced_at"`
	ComplainedAt           *time.Time `gorm:"column:complained_at"`
	ErasedAt               *time.Time `gorm:"column:erased_at"`
	CreatedAt              time.Time  `gorm:"column:created_at"`
	UpdatedAt              time.Time  `gorm:"column:updated_at"`
}

func (NewsletterSubscribersSchema) TableName() string { return "newsletter_subscribers" }

// NewsletterTokensSchema maps newsletter_tokens for AutoMigrate.
type NewsletterTokensSchema struct {
	Id           int64      `gorm:"primaryKey;column:id"`
	SubscriberId int64      `gorm:"column:subscriber_id"`
	Purpose      string     `gorm:"column:purpose"`
	TokenHash    string     `gorm:"column:token_hash"`
	IssueId      *int64     `gorm:"column:issue_id"`
	ExpiresAt    time.Time  `gorm:"column:expires_at"`
	UsedAt       *time.Time `gorm:"column:used_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
}

func (NewsletterTokensSchema) TableName() string { return "newsletter_tokens" }

// NotificationsSchema maps notifications for AutoMigrate.
type NotificationsSchema struct {
	Id          int64      `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	UserId      int64      `gorm:"column:user_id"`
	Type        string     `gorm:"column:type"`
	Title       string     `gorm:"column:title"`
	Body        string     `gorm:"column:body"`
	Preview     string     `gorm:"column:preview"`
	Payload     []byte     `gorm:"column:payload;type:jsonb"`
	ActorUserId *int64     `gorm:"column:actor_user_id"`
	DedupeKey   *string    `gorm:"column:dedupe_key"`
	ReadAt      *time.Time `gorm:"column:read_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
}

func (NotificationsSchema) TableName() string { return "notifications" }

// OutboxEventsSchema maps outbox_events for AutoMigrate.
type OutboxEventsSchema struct {
	Id            int64      `gorm:"primaryKey;column:id"`
	Uuid          uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	Topic         string     `gorm:"column:topic"`
	AggregateType string     `gorm:"column:aggregate_type"`
	AggregateId   *uuid.UUID `gorm:"column:aggregate_id;type:uuid"`
	Payload       []byte     `gorm:"column:payload;type:jsonb"`
	Headers       []byte     `gorm:"column:headers;type:jsonb"`
	OccurredAt    time.Time  `gorm:"column:occurred_at"`
	PublishedAt   *time.Time `gorm:"column:published_at"`
	Attempts      int        `gorm:"column:attempts"`
	LastError     *string    `gorm:"column:last_error"`
	NextAttemptAt time.Time  `gorm:"column:next_attempt_at"`
	FailedAt      *time.Time `gorm:"column:failed_at"`
}

func (OutboxEventsSchema) TableName() string { return "outbox_events" }

// PostRevisionsSchema maps post_revisions for AutoMigrate.
type PostRevisionsSchema struct {
	Id                    int64      `gorm:"primaryKey;column:id"`
	Uuid                  uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	PostId                int64      `gorm:"column:post_id"`
	RevisionNumber        int        `gorm:"column:revision_number"`
	RevisionType          string     `gorm:"column:revision_type"`
	Title                 string     `gorm:"column:title"`
	Slug                  string     `gorm:"column:slug"`
	Excerpt               string     `gorm:"column:excerpt"`
	Content               string     `gorm:"column:content"`
	Status                string     `gorm:"column:status"`
	CommentPolicy         string     `gorm:"column:comment_policy"`
	CategoryUuid          *uuid.UUID `gorm:"column:category_uuid;type:uuid"`
	CoverImageMediaUuid   *uuid.UUID `gorm:"column:cover_image_media_uuid;type:uuid"`
	TagsSnapshot          []byte     `gorm:"column:tags_snapshot;type:jsonb"`
	MediaSnapshot         []byte     `gorm:"column:media_snapshot;type:jsonb"`
	SeoSnapshot           []byte     `gorm:"column:seo_snapshot;type:jsonb"`
	ChangedFields         []byte     `gorm:"column:changed_fields;type:jsonb"`
	Diff                  []byte     `gorm:"column:diff;type:jsonb"`
	Changelog             string     `gorm:"column:changelog"`
	EditorNote            string     `gorm:"column:editor_note"`
	AuthorId              *int64     `gorm:"column:author_id"`
	RestoreFromRevisionId *int64     `gorm:"column:restore_from_revision_id"`
	ImpersonatorId        *int64     `gorm:"column:impersonator_id"`
	RequestId             string     `gorm:"column:request_id"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
}

func (PostRevisionsSchema) TableName() string { return "post_revisions" }

// PostSeoSchema maps post_seo for AutoMigrate.
type PostSeoSchema struct {
	Id                 int64     `gorm:"primaryKey;column:id"`
	Uuid               uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	PostId             int64     `gorm:"column:post_id"`
	SeoTitle           string    `gorm:"column:seo_title"`
	SeoDescription     string    `gorm:"column:seo_description"`
	SeoKeywords        []byte    `gorm:"column:seo_keywords;type:jsonb"`
	OgTitle            string    `gorm:"column:og_title"`
	OgDescription      string    `gorm:"column:og_description"`
	OgImageId          *int64    `gorm:"column:og_image_id"`
	OgUrl              string    `gorm:"column:og_url"`
	TwitterCard        string    `gorm:"column:twitter_card"`
	TwitterTitle       string    `gorm:"column:twitter_title"`
	TwitterDescription string    `gorm:"column:twitter_description"`
	TwitterImageId     *int64    `gorm:"column:twitter_image_id"`
	TwitterCreator     string    `gorm:"column:twitter_creator"`
	CanonicalUrl       string    `gorm:"column:canonical_url"`
	RobotsNoindex      bool      `gorm:"column:robots_noindex"`
	RobotsNofollow     bool      `gorm:"column:robots_nofollow"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

func (PostSeoSchema) TableName() string { return "post_seo" }

// PrivacyRequestsSchema maps privacy_requests for AutoMigrate.
type PrivacyRequestsSchema struct {
	Id          int64      `gorm:"primaryKey;column:id"`
	Uuid        uuid.UUID  `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	UserId      int64      `gorm:"column:user_id"`
	Kind        string     `gorm:"column:kind"`
	Status      string     `gorm:"column:status"`
	StorageKey  *string    `gorm:"column:storage_key"`
	ExpiresAt   *time.Time `gorm:"column:expires_at"`
	Attempts    int        `gorm:"column:attempts"`
	LastError   *string    `gorm:"column:last_error"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

func (PrivacyRequestsSchema) TableName() string { return "privacy_requests" }

// ProcessedMessagesSchema maps processed_messages for AutoMigrate.
type ProcessedMessagesSchema struct {
	Consumer    string    `gorm:"primaryKey;column:consumer"`
	MessageId   string    `gorm:"primaryKey;column:message_id"`
	ProcessedAt time.Time `gorm:"column:processed_at"`
}

func (ProcessedMessagesSchema) TableName() string { return "processed_messages" }

// ScheduledJobRunsSchema maps scheduled_job_runs for AutoMigrate.
type ScheduledJobRunsSchema struct {
	Name           string     `gorm:"primaryKey;column:name"`
	LastStartedAt  time.Time  `gorm:"column:last_started_at"`
	LastFinishedAt *time.Time `gorm:"column:last_finished_at"`
	LastError      *string    `gorm:"column:last_error"`
	Runs           int64      `gorm:"column:runs"`
}

func (ScheduledJobRunsSchema) TableName() string { return "scheduled_job_runs" }

// SettingsSchema maps settings for AutoMigrate.
type SettingsSchema struct {
	Id        int64     `gorm:"primaryKey;column:id"`
	Uuid      uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	Key       string    `gorm:"column:key"`
	Value     []byte    `gorm:"column:value;type:jsonb"`
	Version   int64     `gorm:"column:version"`
	UpdatedBy *int64    `gorm:"column:updated_by"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (SettingsSchema) TableName() string { return "settings" }

// SettingsHistorySchema maps settings_history for AutoMigrate.
type SettingsHistorySchema struct {
	Id            int64     `gorm:"primaryKey;column:id"`
	Uuid          uuid.UUID `gorm:"column:uuid;type:uuid;default:gen_random_uuid()"`
	SettingKey    string    `gorm:"column:setting_key"`
	PreviousValue []byte    `gorm:"column:previous_value;type:jsonb"`
	NewValue      []byte    `gorm:"column:new_value;type:jsonb"`
	Version       int64     `gorm:"column:version"`
	ChangedBy     *int64    `gorm:"column:changed_by"`
	RequestId     string    `gorm:"column:request_id"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (SettingsHistorySchema) TableName() string { return "settings_history" }

// UserPrivacySettingsSchema maps user_privacy_settings for AutoMigrate.
type UserPrivacySettingsSchema struct {
	UserId              int64     `gorm:"primaryKey;column:user_id"`
	VisibilityProfile   string    `gorm:"column:visibility_profile"`
	VisibilityEmail     bool      `gorm:"column:visibility_email"`
	VisibilityContact   bool      `gorm:"column:visibility_contact"`
	SearchAllowIndexing bool      `gorm:"column:search_allow_indexing"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at"`
}

func (UserPrivacySettingsSchema) TableName() string { return "user_privacy_settings" }

// UserProfilesSchema maps user_profiles for AutoMigrate.
type UserProfilesSchema struct {
	UserId                    int64      `gorm:"primaryKey;column:user_id"`
	DisplayName               *string    `gorm:"column:display_name"`
	Bio                       string     `gorm:"column:bio"`
	ContactWebsite            *string    `gorm:"column:contact_website"`
	ContactLocation           *string    `gorm:"column:contact_location"`
	SocialLinks               []byte     `gorm:"column:social_links;type:jsonb"`
	Locale                    string     `gorm:"column:locale"`
	Timezone                  string     `gorm:"column:timezone"`
	MarketingConsent          bool       `gorm:"column:marketing_consent"`
	MarketingConsentUpdatedAt *time.Time `gorm:"column:marketing_consent_updated_at"`
	UpdatedBy                 *int64     `gorm:"column:updated_by"`
	CreatedAt                 time.Time  `gorm:"column:created_at"`
	UpdatedAt                 time.Time  `gorm:"column:updated_at"`
}

func (UserProfilesSchema) TableName() string { return "user_profiles" }

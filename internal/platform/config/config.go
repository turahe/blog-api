// Package config loads and validates runtime configuration from the environment.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/turahe/blog-api/internal/platform/security/secretbox"
)

// DBDriverPostgres is the only supported DB_DRIVER; the migrations use
// PostgreSQL identity columns, gen_random_uuid(), and partial indexes.
const DBDriverPostgres = "postgres"

const envProduction = "production"

const (
	defaultAddress  = "0.0.0.0:8080"
	defaultDBDriver = DBDriverPostgres
)

// Config is the runtime configuration; see docs/deployment/config.md for variables.
type Config struct {
	Environment                   string
	Address                       string
	DBDriver                      string
	DBHost                        string
	DBPort                        int
	DBUser                        string
	DBPassword                    string
	DBName                        string
	DBSSLMode                     string
	DBInstanceConnectionName      string
	DBIAMAuthEnabled              bool
	DBPrivateIPEnabled            bool
	DBGoogleCredentialsSource     string
	RedisDriver                   string
	RedisHost                     string
	RedisPort                     int
	RedisPassword                 string
	RedisDB                       int
	TrustedProxies                []string
	ShutdownTimeout               time.Duration
	ReadTimeout                   time.Duration
	ReadHeaderTimeout             time.Duration
	IdleTimeout                   time.Duration
	DBMaxOpen                     int
	DBMaxIdle                     int
	DBMaxLifetime                 time.Duration
	DBMaxIdleTime                 time.Duration
	SessionKey                    string
	CSRFKey                       string
	Pepper                        string
	JWTPrivateKey                 string
	JWTPublicKey                  string
	JWTIssuer                     string
	AccessTokenTTL                time.Duration
	RefreshTokenTTL               time.Duration
	AuthLoginPerMinute            int
	AuthLoginMaxFailures          int
	AuthLoginLockout              time.Duration
	RBACPolicyReloadInterval      time.Duration
	EncryptionKey                 string
	TwoFactorIssuer               string
	OAuthGoogleClientID           string
	OAuthGoogleClientSecret       string
	OAuthGitHubClientID           string
	OAuthGitHubClientSecret       string
	OAuthRedirectURIs             []string
	AuditRetentionDays            int
	AuditQueueSize                int
	MessageBroker                 string
	KafkaBrokers                  []string
	KafkaConsumerGroup            string
	KafkaTLS                      bool
	KafkaTLSCAPath                string
	KafkaSASLMechanism            string
	KafkaSASLUsername             string
	KafkaSASLPassword             string
	RabbitMQURL                   string
	GooglePubSubProjectID         string
	GooglePubSubCredentialsSource string
	MessageTopicPrefix            string
	S3Endpoint                    string
	S3Region                      string
	S3Bucket                      string
	S3AccessKey                   string
	S3SecretKey                   string
	S3PublicBaseURL               string
	S3Disk                        string
	S3ForcePathStyle              bool
	MediaAllowedMIMETypes         []string
	MediaMaxUploadBytes           int64
	MediaPresignTTL               time.Duration
	ImgproxyURL                   string
	ImgproxyKey                   string
	ImgproxySalt                  string
	MediaTransformWidths          []int
	MediaTransformURLTTL          time.Duration
	PrivacyExportRetention        time.Duration
	PrivacyExportURLTTL           time.Duration
	SearchLanguage                string
	ImpersonationTTL              time.Duration
	NewsletterProvider            string
	NewsletterHTTPEndpoint        string
	NewsletterHTTPSecret          string
	NewsletterSendBatch           int
	CommentsGuestEnabled          bool
	CommentsRequireApproval       bool
	CommentsEditWindow            time.Duration
	CommentsFlagThreshold         int
	CommentsCreatePerMinute       int
	CommentsActionsPerMinute      int
	TurnstileSecretKey            string
	SSEPingInterval               time.Duration
	SSEMaxConcurrentPerUser       int
	OutboxBatchSize               int
	OutboxPollInterval            time.Duration
	OutboxMaxAttempts             int
	OutboxRetention               time.Duration
	ConsumerMaxRetries            int
	ConsumerRetryInterval         time.Duration
	ConsumerRetryMaxInterval      time.Duration
	ConsumerDedupeRetention       time.Duration
	ConsumerConcurrency           int
	ConsumerBreakerFailures       int
	ConsumerBreakerTimeout        time.Duration
	HTTPMaxInFlight               int
	CacheEnabled                  bool
	CacheBypassHeader             bool
	CacheTTLPosts                 time.Duration
	CacheTTLCategories            time.Duration
	CacheTTLTags                  time.Duration
	CacheTTLUsers                 time.Duration
	CacheTTLSettings              time.Duration
	AvatarMaxBytes                int64
	SwaggerEnabled                bool
	SentryDSN                     string
	SentryEnvironment             string
	SentryTracesSampleRate        float64
	SMTPHost                      string
	SMTPPort                      int
	SMTPUsername                  string
	SMTPPassword                  string
	SMTPFrom                      string
	AppPublicURL                  string
	MetricsAddr                   string
}

// SentryEnabled reports whether SENTRY_DSN is set.
func (c Config) SentryEnabled() bool {
	return strings.TrimSpace(c.SentryDSN) != ""
}

// ValidateSentry checks the traces sample rate is a fraction.
func (c Config) ValidateSentry() error {
	if c.SentryTracesSampleRate < 0 || c.SentryTracesSampleRate > 1 {
		return fmt.Errorf("SENTRY_TRACES_SAMPLE_RATE must be between 0 and 1, got %v", c.SentryTracesSampleRate)
	}

	return nil
}

// UsesCloudSQL reports whether Cloud SQL connector settings are active.
func (c Config) UsesCloudSQL() bool {
	return strings.TrimSpace(c.DBInstanceConnectionName) != ""
}

// MessagingEnabled reports whether MESSAGE_BROKER is set.
func (c Config) MessagingEnabled() bool {
	return strings.TrimSpace(c.MessageBroker) != ""
}

// ParseMIMEList splits a comma-separated MIME allowlist and trims whitespace.
func ParseMIMEList(raw string) []string {
	return splitCSV(raw)
}

// maxTransformWidth caps MEDIA_TRANSFORM_WIDTHS entries.
const maxTransformWidth = 8192

// ParseWidths parses a comma-separated list of transform widths. Any entry outside
// 1..8192 makes the whole list invalid (nil), which ValidateMedia reports.
func ParseWidths(raw string) []int {
	var widths []int

	for _, part := range splitCSV(raw) {
		width, err := strconv.Atoi(part)
		if err != nil || width < 1 || width > maxTransformWidth {
			return nil
		}

		widths = append(widths, width)
	}

	return widths
}

// Minimum decoded imgproxy signing key and salt lengths in production.
const (
	minImgproxyKeyBytes  = 32
	minImgproxySaltBytes = 16
)

// MediaTransformsEnabled reports whether media transforms are delegated to imgproxy.
func (c Config) MediaTransformsEnabled() bool {
	return c.MediaEnabled() && c.ImgproxyURL != ""
}

// MediaEnabled reports whether media storage credentials are configured.
func (c Config) MediaEnabled() bool {
	return strings.TrimSpace(c.S3Bucket) != "" &&
		strings.TrimSpace(c.S3AccessKey) != "" &&
		strings.TrimSpace(c.S3SecretKey) != ""
}

// ValidateMedia checks the upload policy and storage target settings.
func (c Config) ValidateMedia() error {
	if !c.MediaEnabled() {
		return nil
	}

	switch c.S3Disk {
	case "minio", "s3", "r2", "do_spaces":
	default:
		return fmt.Errorf("unsupported S3_DISK %q", c.S3Disk)
	}

	if len(c.MediaAllowedMIMETypes) == 0 {
		return errors.New("MEDIA_ALLOWED_MIME_TYPES must not be empty when media is enabled")
	}

	if c.MediaMaxUploadBytes < 1 {
		return errors.New("MEDIA_MAX_UPLOAD_BYTES must be positive")
	}

	if c.MediaPresignTTL <= 0 {
		return errors.New("MEDIA_PRESIGN_TTL must be positive")
	}

	if c.AvatarMaxBytes < 1 {
		return errors.New("AVATAR_MAX_BYTES must be positive")
	}

	if err := c.validatePrivacyExports(); err != nil {
		return err
	}

	return c.validateTransforms()
}

// maxPresignTTL is the longest lifetime S3 accepts for a presigned URL.
const maxPresignTTL = 7 * 24 * time.Hour

func (c Config) validatePrivacyExports() error {
	if c.PrivacyExportRetention <= 0 {
		return errors.New("PRIVACY_EXPORT_RETENTION must be positive")
	}

	if c.PrivacyExportURLTTL <= 0 || c.PrivacyExportURLTTL > maxPresignTTL {
		return errors.New("PRIVACY_EXPORT_URL_TTL must be between 1s and 168h")
	}

	return nil
}

func (c Config) validateTransforms() error {
	if !c.MediaTransformsEnabled() {
		return nil
	}

	if c.ImgproxyKey == "" || c.ImgproxySalt == "" {
		return errors.New("IMGPROXY_KEY and IMGPROXY_SALT are required with IMGPROXY_URL")
	}

	key, keyErr := hex.DecodeString(c.ImgproxyKey)
	salt, saltErr := hex.DecodeString(c.ImgproxySalt)

	if keyErr != nil || saltErr != nil {
		return errors.New("IMGPROXY_KEY and IMGPROXY_SALT must be hex")
	}

	// A short key lets anyone forge transform URLs and spend imgproxy CPU.
	if c.Environment == envProduction && (len(key) < minImgproxyKeyBytes || len(salt) < minImgproxySaltBytes) {
		return fmt.Errorf("IMGPROXY_KEY must be at least %d bytes and IMGPROXY_SALT at least %d bytes in production",
			minImgproxyKeyBytes, minImgproxySaltBytes)
	}

	if len(c.MediaTransformWidths) == 0 {
		return errors.New("MEDIA_TRANSFORM_WIDTHS must list widths between 1 and 8192")
	}

	if c.MediaTransformURLTTL <= 0 {
		return errors.New("MEDIA_TRANSFORM_URL_TTL must be positive")
	}

	return nil
}

// Kafka SASL mechanisms accepted in KAFKA_SASL_MECHANISM.
const (
	KafkaSASLPlain       = "PLAIN"
	KafkaSASLSCRAMSHA256 = "SCRAM-SHA-256"
	KafkaSASLSCRAMSHA512 = "SCRAM-SHA-512"
)

func (c Config) validateKafka() error {
	if len(c.KafkaBrokers) == 0 {
		return errors.New("KAFKA_BROKERS is required when MESSAGE_BROKER=kafka")
	}

	if strings.TrimSpace(c.KafkaConsumerGroup) == "" {
		return errors.New("KAFKA_CONSUMER_GROUP is required when MESSAGE_BROKER=kafka")
	}

	if c.KafkaTLSCAPath != "" && !c.KafkaTLS {
		return errors.New("KAFKA_TLS_CA_PATH needs KAFKA_TLS=true")
	}

	switch c.KafkaSASLMechanism {
	case "":
		if c.KafkaSASLUsername != "" || c.KafkaSASLPassword != "" {
			return errors.New("KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD need KAFKA_SASL_MECHANISM")
		}

		return nil
	case KafkaSASLPlain, KafkaSASLSCRAMSHA256, KafkaSASLSCRAMSHA512:
	default:
		return fmt.Errorf("unsupported KAFKA_SASL_MECHANISM %q (want PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512)", c.KafkaSASLMechanism)
	}

	if c.KafkaSASLUsername == "" || c.KafkaSASLPassword == "" {
		return errors.New("KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD are required with KAFKA_SASL_MECHANISM")
	}

	// PLAIN sends the password as is and SCRAM exposes the exchange to offline guessing.
	if c.Environment == envProduction && !c.KafkaTLS {
		return errors.New("KAFKA_TLS=true is required with KAFKA_SASL_MECHANISM in production")
	}

	return nil
}

// NormalizeDBDriver maps aliases to canonical DB_DRIVER names.
func NormalizeDBDriver(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DBDriverPostgres, "postgresql", "pg":
		return DBDriverPostgres, nil
	default:
		return "", fmt.Errorf("unsupported DB_DRIVER %q (only postgres is supported)", raw)
	}
}

const defaultDBPort = 5432

// DatabaseDSN builds a PostgreSQL DSN from split DB_* settings.
func (c Config) DatabaseDSN() (string, error) {
	if _, err := NormalizeDBDriver(c.DBDriver); err != nil {
		return "", err
	}

	port := c.DBPort
	if port == 0 {
		port = defaultDBPort
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   net.JoinHostPort(c.DBHost, strconv.Itoa(port)),
		Path:   "/" + c.DBName,
	}
	q := u.Query()

	sslmode := c.DBSSLMode
	if sslmode == "" {
		sslmode = "disable"
	}

	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// ValidateDatabase checks split database settings for direct connections.
// Cloud SQL mode only requires instance/user/name (validated elsewhere).
func (c Config) ValidateDatabase() error {
	if c.UsesCloudSQL() {
		return nil
	}

	if _, err := NormalizeDBDriver(c.DBDriver); err != nil {
		return err
	}

	if strings.TrimSpace(c.DBHost) == "" {
		return errors.New("DB_HOST must not be empty")
	}

	port := c.DBPort
	if port == 0 {
		port = defaultDBPort
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("DB_PORT must be between 1 and 65535 (got %d)", port)
	}

	if strings.TrimSpace(c.DBUser) == "" {
		return errors.New("DB_USER must not be empty")
	}

	if strings.TrimSpace(c.DBName) == "" {
		return errors.New("DB_NAME must not be empty")
	}

	sslmode := strings.ToLower(strings.TrimSpace(c.DBSSLMode))
	if sslmode == "" {
		sslmode = "disable"
	}

	if c.Environment == envProduction && sslmode == "disable" {
		return errors.New("DB_SSLMODE cannot be disable in production")
	}

	return nil
}

// ValidateCache rejects negative public-read cache TTLs; zero disables a family.
func (c Config) ValidateCache() error {
	for name, ttl := range map[string]time.Duration{
		"CACHE_TTL_POSTS":      c.CacheTTLPosts,
		"CACHE_TTL_CATEGORIES": c.CacheTTLCategories,
		"CACHE_TTL_TAGS":       c.CacheTTLTags,
		"CACHE_TTL_USERS":      c.CacheTTLUsers,
		"CACHE_TTL_SETTINGS":   c.CacheTTLSettings,
	} {
		if ttl < 0 {
			return fmt.Errorf("%s must be zero or greater (got %s)", name, ttl)
		}
	}

	return nil
}

// RedisURL builds the go-redis connection URL from split Redis settings.
// Both redis and valkey speak the Redis wire protocol, so the URL scheme is always redis://.
func (c Config) RedisURL() string {
	u := &url.URL{
		Scheme: "redis",
		Host:   net.JoinHostPort(c.RedisHost, strconv.Itoa(c.RedisPort)),
		Path:   "/" + strconv.Itoa(c.RedisDB),
	}
	if c.RedisPassword != "" {
		u.User = url.UserPassword("", c.RedisPassword)
	}

	return u.String()
}

// ValidateRedis checks split Redis connection settings.
func (c Config) ValidateRedis() error {
	if c.RedisDriver != "redis" && c.RedisDriver != "valkey" {
		return fmt.Errorf("unsupported REDIS_DRIVER %q (want redis or valkey)", c.RedisDriver)
	}

	if strings.TrimSpace(c.RedisHost) == "" {
		return errors.New("REDIS_HOST must not be empty")
	}

	if c.RedisPort < 1 || c.RedisPort > 65535 {
		return fmt.Errorf("REDIS_PORT must be between 1 and 65535 (got %d)", c.RedisPort)
	}

	if c.RedisDB < 0 {
		return fmt.Errorf("REDIS_DB must be zero or greater (got %d)", c.RedisDB)
	}

	return nil
}

func normalizeMessageBroker(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rabbitmq", "amqp", "rabbit":
		return "rabbitmq"
	case "googlepubsub", "gcp-pubsub", "pubsub":
		return "googlepubsub"
	case "kafka":
		return "kafka"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// ValidateMessaging checks the settings required by the selected broker.
func (c Config) ValidateMessaging() error {
	broker := normalizeMessageBroker(c.MessageBroker)
	if broker == "" {
		return nil
	}

	switch broker {
	case "kafka":
		return c.validateKafka()
	case "rabbitmq":
		if strings.TrimSpace(c.RabbitMQURL) == "" {
			return errors.New("RABBITMQ_URL is required when MESSAGE_BROKER=rabbitmq")
		}
	case "googlepubsub":
		if strings.TrimSpace(c.GooglePubSubProjectID) == "" {
			return errors.New("GOOGLE_PUBSUB_PROJECT_ID is required when MESSAGE_BROKER=googlepubsub")
		}
	default:
		return fmt.Errorf("unsupported MESSAGE_BROKER %q (want kafka, rabbitmq, or googlepubsub)", c.MessageBroker)
	}

	return nil
}

// Load reads the environment, loads the JWT keys, and validates the result.
func Load() (Config, error) { return load(true) }

// LoadBackground is Load for processes that never issue or verify tokens (worker, scheduler,
// migrate, outbox, audit). The JWT keys are not read, so the signing key need not be deployed
// where broker messages are processed.
func LoadBackground() (Config, error) { return load(false) }

func load(withJWTKeys bool) (Config, error) {
	driver := env("DB_DRIVER", defaultDBDriver)

	port := integer("DB_PORT", 0)
	if port == 0 {
		port = defaultDBPort
	}

	cfg := Config{
		Environment:                   env("APP_ENV", "local"),
		Address:                       env("APP_ADDR", defaultAddress),
		DBDriver:                      driver,
		DBHost:                        env("DB_HOST", "127.0.0.1"),
		DBPort:                        port,
		DBUser:                        env("DB_USER", "blog"),
		DBPassword:                    env("DB_PASSWORD", "blog"),
		DBName:                        env("DB_NAME", "blog"),
		DBSSLMode:                     env("DB_SSLMODE", "disable"),
		DBInstanceConnectionName:      env("DB_INSTANCE_CONNECTION_NAME", ""),
		DBIAMAuthEnabled:              boolEnv("DB_IAM_AUTH_ENABLED", false),
		DBPrivateIPEnabled:            boolEnv("DB_PRIVATE_IP_ENABLED", true),
		DBGoogleCredentialsSource:     env("DB_GOOGLE_CREDENTIALS_SOURCE", "workload-identity"),
		RedisDriver:                   strings.ToLower(env("REDIS_DRIVER", "redis")),
		RedisHost:                     env("REDIS_HOST", "127.0.0.1"),
		RedisPort:                     integer("REDIS_PORT", 6379),
		RedisPassword:                 env("REDIS_PASSWORD", ""),
		RedisDB:                       integer("REDIS_DB", 0),
		TrustedProxies:                splitCSV(os.Getenv("APP_TRUSTED_PROXIES")),
		ShutdownTimeout:               duration("APP_SHUTDOWN_TIMEOUT", 15*time.Second),
		ReadTimeout:                   duration("APP_READ_TIMEOUT", 15*time.Second),
		ReadHeaderTimeout:             duration("APP_READ_HEADER_TIMEOUT", 5*time.Second),
		IdleTimeout:                   duration("APP_IDLE_TIMEOUT", 60*time.Second),
		DBMaxOpen:                     integer("DB_POOL_MAX_OPEN", 25),
		DBMaxIdle:                     integer("DB_POOL_MAX_IDLE", 10),
		DBMaxLifetime:                 durationOrSeconds("DB_POOL_MAX_LIFETIME", "DB_POOL_MAX_LIFETIME_SECONDS", 30*time.Minute),
		DBMaxIdleTime:                 durationOrSeconds("DB_POOL_MAX_IDLE_TIME", "DB_POOL_MAX_IDLETIME_SECONDS", 10*time.Minute),
		SessionKey:                    env("APP_SESSION_KEY", ""),
		CSRFKey:                       env("APP_CSRF_KEY", ""),
		Pepper:                        env("APP_PEPPER", ""),
		JWTIssuer:                     env("APP_JWT_ISSUER", "blog-api"),
		AccessTokenTTL:                duration("APP_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:               duration("APP_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		AuthLoginPerMinute:            integer("AUTH_LOGIN_PER_MINUTE", 10),
		AuthLoginMaxFailures:          integer("AUTH_LOGIN_MAX_FAILURES", 5),
		AuthLoginLockout:              duration("AUTH_LOGIN_LOCKOUT", 15*time.Minute),
		RBACPolicyReloadInterval:      duration("RBAC_POLICY_RELOAD_INTERVAL", 30*time.Second),
		EncryptionKey:                 env("APP_ENCRYPTION_KEY", ""),
		TwoFactorIssuer:               env("AUTH_2FA_ISSUER", "Blog"),
		OAuthGoogleClientID:           env("OAUTH_GOOGLE_CLIENT_ID", ""),
		OAuthGoogleClientSecret:       env("OAUTH_GOOGLE_CLIENT_SECRET", ""),
		OAuthGitHubClientID:           env("OAUTH_GITHUB_CLIENT_ID", ""),
		OAuthGitHubClientSecret:       env("OAUTH_GITHUB_CLIENT_SECRET", ""),
		OAuthRedirectURIs:             splitCSV(os.Getenv("OAUTH_REDIRECT_URIS")),
		AuditRetentionDays:            integer("AUDIT_RETENTION_DAYS", 395),
		AuditQueueSize:                integer("AUDIT_QUEUE_SIZE", 1024),
		MessageBroker:                 strings.ToLower(env("MESSAGE_BROKER", "")),
		KafkaBrokers:                  splitCSV(os.Getenv("KAFKA_BROKERS")),
		KafkaConsumerGroup:            env("KAFKA_CONSUMER_GROUP", "blog-api"),
		RabbitMQURL:                   env("RABBITMQ_URL", ""),
		GooglePubSubProjectID:         env("GOOGLE_PUBSUB_PROJECT_ID", ""),
		GooglePubSubCredentialsSource: env("GOOGLE_PUBSUB_CREDENTIALS_SOURCE", "workload-identity"),
		MessageTopicPrefix:            env("MESSAGE_TOPIC_PREFIX", "blog."),
		S3Endpoint:                    env("S3_ENDPOINT", "http://127.0.0.1:9000"),
		S3Region:                      env("S3_REGION", "auto"),
		S3Bucket:                      env("S3_BUCKET", ""),
		S3AccessKey:                   env("S3_ACCESS_KEY", ""),
		S3SecretKey:                   env("S3_SECRET_KEY", ""),
		S3PublicBaseURL:               env("S3_PUBLIC_BASE_URL", ""),
		S3Disk:                        strings.ToLower(env("S3_DISK", "minio")),
		S3ForcePathStyle:              boolEnv("S3_FORCE_PATH_STYLE", true),
		MediaAllowedMIMETypes:         ParseMIMEList(env("MEDIA_ALLOWED_MIME_TYPES", "image/jpeg,image/png,image/webp,image/gif")),
		ImgproxyURL:                   strings.TrimSpace(env("IMGPROXY_URL", "")),
		ImgproxyKey:                   strings.TrimSpace(env("IMGPROXY_KEY", "")),
		ImgproxySalt:                  strings.TrimSpace(env("IMGPROXY_SALT", "")),
		MediaTransformWidths:          ParseWidths(env("MEDIA_TRANSFORM_WIDTHS", "64,128,256,320,480,640,768,1024,1280,1536,1920")),
		MediaTransformURLTTL:          duration("MEDIA_TRANSFORM_URL_TTL", 24*time.Hour),
		MediaMaxUploadBytes:           int64(integer("MEDIA_MAX_UPLOAD_BYTES", 10<<20)),
		MediaPresignTTL:               duration("MEDIA_PRESIGN_TTL", 15*time.Minute),
		PrivacyExportRetention:        duration("PRIVACY_EXPORT_RETENTION", 72*time.Hour),
		PrivacyExportURLTTL:           duration("PRIVACY_EXPORT_URL_TTL", 15*time.Minute),
		SearchLanguage:                strings.ToLower(strings.TrimSpace(env("SEARCH_LANGUAGE", "simple"))),
		ImpersonationTTL:              duration("IMPERSONATION_TTL", time.Hour),
		CommentsGuestEnabled:          boolEnv("COMMENTS_GUEST_ENABLED", false),
		CommentsRequireApproval:       boolEnv("COMMENTS_REQUIRE_APPROVAL", false),
		CommentsEditWindow:            duration("COMMENTS_EDIT_WINDOW", 15*time.Minute),
		CommentsFlagThreshold:         integer("COMMENTS_FLAG_THRESHOLD", 3),
		CommentsCreatePerMinute:       integer("COMMENTS_CREATE_PER_MINUTE", 6),
		CommentsActionsPerMinute:      integer("COMMENTS_ACTIONS_PER_MINUTE", 30),
		TurnstileSecretKey:            strings.TrimSpace(env("TURNSTILE_SECRET_KEY", "")),
		SSEPingInterval:               duration("SSE_PING_INTERVAL", 15*time.Second),
		SSEMaxConcurrentPerUser:       integer("SSE_MAX_CONCURRENT_PER_USER", 3),
		AvatarMaxBytes:                int64(integer("AVATAR_MAX_BYTES", 5<<20)),
		SentryDSN:                     env("SENTRY_DSN", ""),
		SentryTracesSampleRate:        float("SENTRY_TRACES_SAMPLE_RATE", 0.1),
		SMTPHost:                      env("SMTP_HOST", ""),
		SMTPPort:                      integer("SMTP_PORT", 1025),
		SMTPUsername:                  env("SMTP_USERNAME", ""),
		SMTPPassword:                  env("SMTP_PASSWORD", ""),
		SMTPFrom:                      env("SMTP_FROM", "Blog <blog@localhost>"),
		AppPublicURL:                  env("APP_PUBLIC_URL", "http://127.0.0.1:8080"),
		MetricsAddr:                   env("METRICS_ADDR", ""),
		HTTPMaxInFlight:               integer("HTTP_MAX_INFLIGHT", 0),
	}
	cfg.SwaggerEnabled = boolEnv("APP_SWAGGER_ENABLED", cfg.Environment == "local")
	cfg.SentryEnvironment = env("SENTRY_ENVIRONMENT", cfg.Environment)
	cfg.loadCache()
	cfg.loadWorker()
	cfg.loadNewsletter()
	cfg.loadKafkaSecurity()

	if withJWTKeys {
		if err := cfg.loadJWTKeys(); err != nil {
			return Config{}, err
		}
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// loadCache reads the Redis read-cache switch and per-family TTLs.
func (c *Config) loadCache() {
	c.CacheEnabled = boolEnv("CACHE_ENABLED", true)
	c.CacheBypassHeader = boolEnv("CACHE_BYPASS_HEADER", false)
	c.CacheTTLPosts = duration("CACHE_TTL_POSTS", time.Minute)
	c.CacheTTLCategories = duration("CACHE_TTL_CATEGORIES", 10*time.Minute)
	c.CacheTTLTags = duration("CACHE_TTL_TAGS", 10*time.Minute)
	c.CacheTTLUsers = duration("CACHE_TTL_USERS", 15*time.Minute)
	c.CacheTTLSettings = duration("CACHE_TTL_SETTINGS", 10*time.Minute)
}

// loadKafkaSecurity reads the Kafka TLS and SASL settings.
func (c *Config) loadKafkaSecurity() {
	c.KafkaTLS = boolEnv("KAFKA_TLS", false)
	c.KafkaTLSCAPath = env("KAFKA_TLS_CA_PATH", "")
	c.KafkaSASLMechanism = strings.ToUpper(env("KAFKA_SASL_MECHANISM", ""))
	c.KafkaSASLUsername = env("KAFKA_SASL_USERNAME", "")
	c.KafkaSASLPassword = env("KAFKA_SASL_PASSWORD", "")
}

// loadWorker reads the outbox relay and message consumer settings.
func (c *Config) loadWorker() {
	c.OutboxBatchSize = integer("OUTBOX_BATCH_SIZE", 100)
	c.OutboxPollInterval = duration("OUTBOX_POLL_INTERVAL", time.Second)
	c.OutboxMaxAttempts = integer("OUTBOX_MAX_ATTEMPTS", 10)
	c.OutboxRetention = duration("OUTBOX_RETENTION", 7*24*time.Hour)
	c.ConsumerMaxRetries = integer("CONSUMER_MAX_RETRIES", 3)
	c.ConsumerRetryInterval = duration("CONSUMER_RETRY_INTERVAL", time.Second)
	c.ConsumerRetryMaxInterval = duration("CONSUMER_RETRY_MAX_INTERVAL", 30*time.Second)
	c.ConsumerDedupeRetention = duration("CONSUMER_DEDUPE_RETENTION", 7*24*time.Hour)
	c.ConsumerConcurrency = max(integer("CONSUMER_CONCURRENCY", 1), 1)
	c.ConsumerBreakerFailures = integer("CONSUMER_BREAKER_FAILURES", 5)
	c.ConsumerBreakerTimeout = duration("CONSUMER_BREAKER_TIMEOUT", 30*time.Second)
}

func (c *Config) loadJWTKeys() error {
	privateKey, err := pemFromEnvOrFile("APP_JWT_PRIVATE_KEY", "APP_JWT_PRIVATE_KEY_PATH")
	if err != nil {
		return fmt.Errorf("load JWT private key: %w", err)
	}

	publicKey, err := pemFromEnvOrFile("APP_JWT_PUBLIC_KEY", "APP_JWT_PUBLIC_KEY_PATH")
	if err != nil {
		return fmt.Errorf("load JWT public key: %w", err)
	}

	if privateKey == "" || publicKey == "" {
		return errors.New("JWT ES256 keys are required: set APP_JWT_PRIVATE_KEY or APP_JWT_PRIVATE_KEY_PATH, and APP_JWT_PUBLIC_KEY or APP_JWT_PUBLIC_KEY_PATH")
	}

	c.JWTPrivateKey = privateKey
	c.JWTPublicKey = publicKey

	return nil
}

// validate checks cross-field rules and fills the local-only session key fallback.
func (c *Config) validate() error {
	if err := c.validateSecrets(); err != nil {
		return err
	}

	if c.Environment == envProduction && c.UsesCloudSQL() &&
		(c.DBInstanceConnectionName == "" || c.DBName == "" || c.DBUser == "") {
		return errors.New("cloud SQL requires DB_INSTANCE_CONNECTION_NAME, DB_NAME, and DB_USER in production")
	}

	if err := c.ValidateDatabase(); err != nil {
		return err
	}

	if c.DBMaxOpen < 1 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return fmt.Errorf("invalid database pool limits: idle=%d open=%d", c.DBMaxIdle, c.DBMaxOpen)
	}

	for _, check := range []func() error{c.ValidateRedis, c.ValidateMessaging, c.ValidateMedia, c.ValidateSentry, c.ValidateCache, c.ValidateOAuth, c.ValidateAudit, c.ValidateSearch, c.ValidateImpersonation, c.ValidateNewsletter} {
		if err := check(); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) validateSecrets() error {
	if c.Address == "" {
		return errors.New("APP_ADDR must not be empty")
	}

	if len(c.SessionKey) < 32 {
		if c.Environment == envProduction {
			return errors.New("APP_SESSION_KEY must be at least 32 characters")
		}

		c.SessionKey = "local-dev-session-key-32bytes-min!!"
	}

	if c.EncryptionKey != "" {
		if _, err := secretbox.ParseKey(c.EncryptionKey); err != nil {
			return fmt.Errorf("APP_ENCRYPTION_KEY: %w", err)
		}
	}

	return nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

// pemFromEnvOrFile loads a PEM from an inline env var or a path env var.
// Path wins when both are set. Inline values may use literal "\n" for newlines.
func pemFromEnvOrFile(inlineKey, pathKey string) (string, error) {
	path := strings.TrimSpace(os.Getenv(pathKey))
	if path != "" {
		// bearer:disable go_gosec_filesystem_filereadtaint
		raw, err := os.ReadFile(path) //nolint:gosec // G304: path comes from operator-controlled *_PATH env config
		if err != nil {
			return "", fmt.Errorf("read %s (%s): %w", pathKey, path, err)
		}

		pem := strings.TrimSpace(string(raw))
		if pem == "" {
			return "", fmt.Errorf("%s (%s) is empty", pathKey, path)
		}

		return pem, nil
	}

	inline := strings.TrimSpace(os.Getenv(inlineKey))
	if inline == "" {
		return "", nil
	}

	return strings.ReplaceAll(inline, `\n`, "\n"), nil
}

func boolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func integer(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func float(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}

	return parsed
}

func duration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func durationOrSeconds(durationKey, secondsKey string, fallback time.Duration) time.Duration {
	if value := strings.TrimSpace(os.Getenv(durationKey)); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}

	if value := strings.TrimSpace(os.Getenv(secondsKey)); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	return fallback
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

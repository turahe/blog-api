// Package config loads and validates runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Canonical DB_DRIVER values returned by NormalizeDBDriver.
const (
	DBDriverPostgres  = "postgres"
	DBDriverMySQL     = "mysql"
	DBDriverSQLServer = "sqlserver"
)

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
	MessageBroker                 string
	KafkaBrokers                  []string
	KafkaConsumerGroup            string
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
	CommentsGuestEnabled          bool
	CommentsRequireApproval       bool
	CommentsEditWindow            time.Duration
	CommentsFlagThreshold         int
	CommentsCreatePerMinute       int
	CommentsActionsPerMinute      int
	SwaggerEnabled                bool
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

	return nil
}

// NormalizeDBDriver maps aliases to canonical DB_DRIVER names.
func NormalizeDBDriver(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DBDriverPostgres, "postgresql", "pg":
		return DBDriverPostgres, nil
	case DBDriverMySQL, "mariadb":
		return DBDriverMySQL, nil
	case DBDriverSQLServer, "mssql":
		return DBDriverSQLServer, nil
	default:
		return "", fmt.Errorf("unsupported DB_DRIVER %q (want postgres, mysql, or sqlserver)", raw)
	}
}

func defaultDBPort(driver string) int {
	switch driver {
	case DBDriverMySQL:
		return 3306
	case DBDriverSQLServer:
		return 1433
	default:
		return 5432
	}
}

// DatabaseDSN builds a driver-specific DSN from split DB_* settings.
func (c Config) DatabaseDSN() (string, error) {
	driver, err := NormalizeDBDriver(c.DBDriver)
	if err != nil {
		return "", err
	}

	port := c.DBPort
	if port == 0 {
		port = defaultDBPort(driver)
	}

	hostPort := net.JoinHostPort(c.DBHost, strconv.Itoa(port))

	switch driver {
	case DBDriverPostgres:
		u := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(c.DBUser, c.DBPassword),
			Host:   hostPort,
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
	case DBDriverMySQL:
		userInfo := url.UserPassword(c.DBUser, c.DBPassword)
		return fmt.Sprintf("%s@tcp(%s)/%s?parseTime=true", userInfo.String(), hostPort, c.DBName), nil
	case DBDriverSQLServer:
		u := &url.URL{
			Scheme: "sqlserver",
			User:   url.UserPassword(c.DBUser, c.DBPassword),
			Host:   hostPort,
		}
		q := u.Query()
		q.Set("database", c.DBName)
		u.RawQuery = q.Encode()

		return u.String(), nil
	default:
		return "", fmt.Errorf("unsupported DB_DRIVER %q", driver)
	}
}

// ValidateDatabase checks split database settings for direct connections.
// Cloud SQL mode only requires instance/user/name (validated elsewhere).
func (c Config) ValidateDatabase() error {
	if c.UsesCloudSQL() {
		return nil
	}

	driver, err := NormalizeDBDriver(c.DBDriver)
	if err != nil {
		return err
	}

	if strings.TrimSpace(c.DBHost) == "" {
		return errors.New("DB_HOST must not be empty")
	}

	port := c.DBPort
	if port == 0 {
		port = defaultDBPort(driver)
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

	if driver == DBDriverPostgres {
		sslmode := strings.ToLower(strings.TrimSpace(c.DBSSLMode))
		if sslmode == "" {
			sslmode = "disable"
		}

		if c.Environment == "production" && sslmode == "disable" {
			return errors.New("DB_SSLMODE cannot be disable in production")
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
		if len(c.KafkaBrokers) == 0 {
			return errors.New("KAFKA_BROKERS is required when MESSAGE_BROKER=kafka")
		}

		if strings.TrimSpace(c.KafkaConsumerGroup) == "" {
			return errors.New("KAFKA_CONSUMER_GROUP is required when MESSAGE_BROKER=kafka")
		}
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
func Load() (Config, error) {
	driver := env("DB_DRIVER", defaultDBDriver)
	normalized, _ := NormalizeDBDriver(driver)

	port := integer("DB_PORT", 0)
	if port == 0 {
		port = defaultDBPort(normalized)
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
		MediaMaxUploadBytes:           int64(integer("MEDIA_MAX_UPLOAD_BYTES", 10<<20)),
		MediaPresignTTL:               duration("MEDIA_PRESIGN_TTL", 15*time.Minute),
		CommentsGuestEnabled:          boolEnv("COMMENTS_GUEST_ENABLED", false),
		CommentsRequireApproval:       boolEnv("COMMENTS_REQUIRE_APPROVAL", false),
		CommentsEditWindow:            duration("COMMENTS_EDIT_WINDOW", 15*time.Minute),
		CommentsFlagThreshold:         integer("COMMENTS_FLAG_THRESHOLD", 3),
		CommentsCreatePerMinute:       integer("COMMENTS_CREATE_PER_MINUTE", 6),
		CommentsActionsPerMinute:      integer("COMMENTS_ACTIONS_PER_MINUTE", 30),
	}
	cfg.SwaggerEnabled = boolEnv("APP_SWAGGER_ENABLED", cfg.Environment == "local")

	if err := cfg.loadJWTKeys(); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
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

	c.JWTPrivateKey = privateKey
	c.JWTPublicKey = publicKey

	return nil
}

// validate checks cross-field rules and fills the local-only session key fallback.
func (c *Config) validate() error {
	if err := c.validateSecrets(); err != nil {
		return err
	}

	if c.Environment == "production" && c.UsesCloudSQL() &&
		(c.DBInstanceConnectionName == "" || c.DBName == "" || c.DBUser == "") {
		return errors.New("cloud SQL requires DB_INSTANCE_CONNECTION_NAME, DB_NAME, and DB_USER in production")
	}

	if err := c.ValidateDatabase(); err != nil {
		return err
	}

	if c.DBMaxOpen < 1 || c.DBMaxIdle < 0 || c.DBMaxIdle > c.DBMaxOpen {
		return fmt.Errorf("invalid database pool limits: idle=%d open=%d", c.DBMaxIdle, c.DBMaxOpen)
	}

	for _, check := range []func() error{c.ValidateRedis, c.ValidateMessaging, c.ValidateMedia} {
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
		if c.Environment == "production" {
			return errors.New("APP_SESSION_KEY must be at least 32 characters")
		}

		c.SessionKey = "local-dev-session-key-32bytes-min!!"
	}

	if c.JWTPrivateKey == "" || c.JWTPublicKey == "" {
		return errors.New("JWT RSA keys are required: set APP_JWT_PRIVATE_KEY or APP_JWT_PRIVATE_KEY_PATH, and APP_JWT_PUBLIC_KEY or APP_JWT_PUBLIC_KEY_PATH")
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

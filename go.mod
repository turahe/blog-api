module github.com/turahe/blog-api

go 1.26.5

require (
	cloud.google.com/go/cloudsqlconn v1.23.0
	github.com/ThreeDotsLabs/watermill v1.5.2
	github.com/ThreeDotsLabs/watermill-amqp/v3 v3.1.0
	github.com/ThreeDotsLabs/watermill-googlecloud/v2 v2.0.1
	github.com/ThreeDotsLabs/watermill-kafka/v3 v3.1.4
	github.com/aws/aws-sdk-go-v2 v1.43.2
	github.com/aws/aws-sdk-go-v2/config v1.32.33
	github.com/aws/aws-sdk-go-v2/credentials v1.19.32
	github.com/aws/aws-sdk-go-v2/service/s3 v1.106.2
	github.com/aws/smithy-go v1.27.5
	github.com/casbin/casbin/v2 v2.135.0
	github.com/gin-gonic/gin v1.12.0
	github.com/go-sql-driver/mysql v1.10.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/lib/pq v1.12.3
	github.com/microsoft/go-mssqldb v1.10.0
	github.com/pressly/goose/v3 v3.27.3
	github.com/redis/go-redis/v9 v9.21.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	golang.org/x/crypto v0.54.0
	google.golang.org/api v0.291.0
	gorm.io/driver/mysql v1.6.0
	gorm.io/driver/postgres v1.6.2
	gorm.io/driver/sqlserver v1.6.4
	gorm.io/gorm v1.31.2
)

require (
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.22.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.12.0 // indirect
	cloud.google.com/go/pubsub/v2 v2.6.1 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/IBM/sarama v1.60.1 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.15 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.33 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.33 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.33 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.14 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.26 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.33 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.34 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.5.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.33.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.38.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.45.2 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic v1.15.2 // indirect
	github.com/bytedance/sonic/loader v0.5.2 // indirect
	github.com/casbin/govaluate v1.10.0 // indirect
	github.com/cenkalti/backoff/v3 v3.2.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudwego/base64x v0.1.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dnwe/otelsarama v0.0.0-20240308230250-9388d9d40bc0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/eapache/go-resiliency v1.7.0 // indirect
	github.com/eapache/go-xerial-snappy v0.0.0-20230731223053-c322873962e3 // indirect
	github.com/eapache/queue v1.1.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/gin-contrib/sse v1.1.1 // indirect
	github.com/glebarez/go-sqlite v1.20.3 // indirect
	github.com/glebarez/sqlite v1.7.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.3 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.19 // indirect
	github.com/googleapis/gax-go/v2 v2.23.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/leodido/go-urn v1.5.0 // indirect
	github.com/lithammer/shortuuid/v3 v3.0.7 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.61.0 // indirect
	github.com/rabbitmq/amqp091-go v1.13.0 // indirect
	github.com/rcrowley/go-metrics v0.0.0-20250401214520-65e299d6c5c9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	go.mongodb.org/mongo-driver/v2 v2.8.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.69.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/arch v0.29.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto v0.0.0-20260729162451-8efbd57d26e0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260729162451-8efbd57d26e0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260729162451-8efbd57d26e0 // indirect
	google.golang.org/grpc v1.83.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/plugin/dbresolver v1.5.3 // indirect
	modernc.org/libc v1.74.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.54.0 // indirect
)

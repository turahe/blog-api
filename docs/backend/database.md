# Database

Model-layer conventions (domain vs GORM vs DTOs) and the entity catalog:
[model.md](./model.md). Relationship diagrams: [ERD.md](./ERD.md).

## Primary Store

PostgreSQL is the source of truth for:

- users
- roles
- permissions
- user_roles
- role_permissions
- posts
- post_media
- categories
- tags
- post_tags
- comments
- media_assets
- media_transforms
- settings
- settings_history
- impersonation_sessions
- post_revisions
- post_seo
- user_profiles
- user_privacy_settings
- user_password_history
- password_reset_tokens
- user_activity
- user_activity_daily (materialized view)
- audit_logs
- outbox_events

## Design Rules

- every entity table has `id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY` and `uuid uuid NOT NULL UNIQUE DEFAULT gen_random_uuid()`; foreign keys are `bigint` and reference `id`
- only the `uuid` is an external identifier (API, JWT subject, Casbin subject, events); bigint ids stay inside the database and persistence adapters
- pure join tables (`user_roles`, `role_permissions`, `post_tags`) keep composite primary keys over their bigint foreign keys
- keep unique indexes on slugs, emails, role names, and permission keys
- keep indexed lookup fields for media storage key, checksum, and frequently queried tags
- use timestamps consistently
- soft delete only where it has real product value

## Migration Rules

- keep migrations explicit
- review indexes with every schema change
- avoid hidden auto-migration behavior in production
- apply migrations with `app migrate up`
- rollback with `app migrate down`
- `app migrate down` must support reversible migrations and avoid silent data loss
- seeding initial or reference data must use `app seed`, not migration files

## Redis Usage

Redis is not the source of truth. Use it for:

- cache
- rate limiting
- short-lived session or security state
- coordination for background work
- transformed media cache entries when Redis caching is enabled

## Google Cloud SQL Connectivity (`cloud.google.com/go/cloudsqlconn`)

This project targets Google Cloud SQL for managed relational data in production deployments.
All database connections (PostgreSQL only) use the **`cloud.google.com/go/cloudsqlconn`** Go connector (v1.x / v2+) to handle
IAM authentication, certificate rotation, private IP peering, and short-lived TLS
credentials without ever putting static database passwords into environment variables or
secrets manager entries. The canonical connector package and latest usage guidance lives at
<https://pkg.go.dev/cloud.google.com/go/cloudsqlconn> and
<https://cloud.google.com/sql/docs/postgres/connect-connectors#go_1>.

### Supported Database Dialect

Only PostgreSQL is supported. MySQL and SQL Server drivers were removed because the goose
migrations use identity columns, `gen_random_uuid()`, and partial indexes.

| Dialect (`DB_DRIVER`) | Driver | GORM driver package | Cloud SQL connector integration |
|-----------------------|--------|---------------------|---------------------------------|
| `postgres` — PostgreSQL 15+ (source of truth) | `github.com/jackc/pgx/v5/stdlib` | `gorm.io/driver/postgres` | `cloud.google.com/go/cloudsqlconn/postgres/pgxv5` — `RegisterDriver("cloudsql-postgres", WithIAMAuthN(), WithPrivateIP())` then `sql.Open` + GORM `Conn`. |

Local / non-Cloud-SQL: set split `DB_*` fields (`DB_DRIVER`, `DB_HOST`, `DB_PORT`, `DB_USER`,
`DB_PASSWORD`, `DB_NAME`, and for PostgreSQL `DB_SSLMODE`). Cloud SQL: set
`DB_INSTANCE_CONNECTION_NAME` (and related `DB_*` vars); `internal/platform/database` opens via
the connector and wraps `*sql.DB` for GORM.

Reference docs:

- Cloud SQL Go connector overview: <https://cloud.google.com/sql/docs/postgres/connect-connectors>
- PostgreSQL connector guide: <https://cloud.google.com/sql/docs/postgres/samples/cloud-sql-postgres-databasesql-connect-connector>
- IAM database authentication for PostgreSQL: <https://cloud.google.com/sql/docs/postgres/iam-logins>
- Private IP setup overview: <https://cloud.google.com/sql/docs/postgres/private-ip>

### Connection via Private IP (Production Default)

Private IP is the **required production path** for this project. All Cloud SQL instances
are provisioned without a public IP, in the same VPC as the GKE / Cloud Run workload,
peered with Private Service Connect so traffic never leaves Google's backbone.

#### Required configuration parameters

All parameters are environment variables prefixed `DB_`; no config files contain database
secrets or passwords.

| Variable | Example | Purpose |
|----------|---------|---------|
| `DB_DRIVER` | `postgres` | Only `postgres` is accepted (default) |
| `DB_HOST` | `127.0.0.1` | Direct-mode hostname (ignored when Cloud SQL is enabled) |
| `DB_PORT` | `5432` | Direct-mode port; defaults to 5432 |
| `DB_USER` | `blog-iam@my-project.iam` | IAM service account or database user |
| `DB_PASSWORD` | *(secret)* | Required when IAM auth is disabled |
| `DB_NAME` | `blog` | Database name inside the Cloud SQL instance |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode for direct connections only |
| `DB_IAM_AUTH_ENABLED` | `true` | Enables IAM DB auth; do NOT set `DB_PASSWORD`. Prefer `true` in prod. |
| `DB_INSTANCE_CONNECTION_NAME` | `my-project:us-central1:blog-pg-01` | Fully qualified Cloud SQL instance name: `project:region:instance`. When set, opens via `cloudsqlconn` instead of direct `DB_HOST`/`DB_PORT`. |
| `DB_PRIVATE_IP_ENABLED` | `true` | When true, passes `cloudsqlconn.WithPrivateIP()` to the Dialer so all connections route over private VPC IP — **never falls back to public IP**. |
| `DB_GOOGLE_CREDENTIALS_SOURCE` | `workload-identity` (default) or `adc` or `path:/secrets/sa-key.json` | How cloudsqlconn resolves its Google credentials. In GKE we use Workload Identity; in Cloud Run, the runtime service account (ADC); locally, developer ADC via `gcloud auth application-default login`. |
| `DB_POOL_MAX_OPEN` | `25` | `sql.DB.SetMaxOpenConns` value (see §"Connection pooling best practices"). |
| `DB_POOL_MAX_IDLE` | `10` | `sql.DB.SetMaxIdleConns`. |
| `DB_POOL_MAX_LIFETIME` / `DB_POOL_MAX_LIFETIME_SECONDS` | `30m` / `1800` | `sql.DB.SetConnMaxLifetime` (well under the 1-hour Cloud SQL connector cert rotation window). |
| `DB_POOL_MAX_IDLE_TIME` / `DB_POOL_MAX_IDLETIME_SECONDS` | `10m` / `600` | `sql.DB.SetConnMaxIdleTime` (forces refresh of idle certificate material). |
| `DB_CONNECT_TIMEOUT_SECONDS` | `15` | Per-dial timeout for the Dialer. Cloud SQL connector already enforces timeouts internally; this caps worst-case connect latency. |
| `DB_TLS_SERVER_CA_MODE` | `enforce-connector-mtls` (default) | Not user-changeable; cloudsqlconn always uses ephemeral MTLS certificates, so we never add a custom CA or client key to the DSN. |

#### Network permission requirements

- **VPC peering**: The application project VPC must peer with `servicenetworking.googleapis.com`
  using a reserved `/16` (or larger) address range allocated for Private Service Connect.
  <https://cloud.google.com/vpc/docs/configure-private-services-access>
- **Firewall (VPC firewall rules / GCP firewall)**:
  - Allow **egress TCP 5432** (PostgreSQL) from the workload subnet CIDR to the
    `servicenetworking` peered range. Cloud SQL inbound allows the same from the workload
    service account via the database IAM binding.
- **Workload identity (GKE) / Runtime service account (Cloud Run)**: the workload service
  account must have `roles/cloudsql.instanceUser` on the Cloud SQL instance (to allow
  private IP connects) **and** `roles/cloudsql.client` on the project. Separate bindings are
  needed for the IAM database login (see §Authentication).
- **Connectivity test**: use the GCP console `Network Intelligence → Connectivity Tests`
  (or `gcloud network-management connectivity-tests create`) to validate `TCP 5432`
  reachability from the workload instance to the Cloud SQL private IP before deploying.

#### Private IP connection process step-by-step

1. **Startup**: `app serve` / `app migrate up` entrypoint invokes
   `internal/platform/database.Open`, which:
   1. Resolves `DB_DRIVER` and either split direct `DB_*` fields or Cloud SQL env (`DB_INSTANCE_CONNECTION_NAME`, …).
   2. For Cloud SQL: registers the driver via the `cloudsqlconn` `pgxv5` helper with `WithIAMAuthN()` and `WithDefaultDialOptions(WithPrivateIP())` when private IP is enabled.
   3. Hands `*sql.DB` to the matching GORM dialector.
2. **IAM token acquisition**: On the first `Dial()`, cloudsqlconn exchanges the workload
   service account token for a short-lived (1 hour) X.509 ephemeral client certificate,
   signed by Google's CA, bound to the Cloud SQL instance identity. Certificates are
   transparently refreshed ~4 minutes before expiry — application code never reads them.
3. **DNS / instance resolution**: `dialer.Dial(ctx, DB_INSTANCE_CONNECTION_NAME)` internally
   calls `sqladmin.connect.get` to discover the instance's private IP endpoint (via the
   `WithPrivateIP()` option). The returned `net.Conn` already has mTLS applied on both sides.
4. **Database login**: `database/sql` opens a connection over that TLS socket. Because we
   used `cloudsqlconn.WithIAMAuthN()`, the login handshake presents the IAM identity
   (e.g. `blog-iam@my-project.iam` for PostgreSQL) and requests IAM database
   authentication; no static password is ever sent.
5. **Pool assignment**: The successfully logged-in connection is handed to `*sql.DB` which
   applies `SetMaxOpenConns / SetMaxIdleConns / SetConnMaxLifetime / SetConnMaxIdleTime`
   per the pool parameters above.

### Integration steps, dependency configuration, authentication, pooling best practices

#### Dependency configuration (Go `go.mod`)

```
require (
    cloud.google.com/go/cloudsqlconn v1.22.x            # Go connector
    github.com/jackc/pgx/v5                            # PostgreSQL
    gorm.io/driver/postgres
    gorm.io/gorm
)
# helper (same module): cloudsqlconn/postgres/pgxv5
```

Notes:

- When using `jackc/pgx/v5/stdlib`, the connector registration pattern via
  `cloudsqlconn.NewDialer(...)` with a custom driver via `pgconn.RegisterDialer` is
  preferred over the legacy DSN string approach because it integrates cleanly with GORM's
  `postgres.New( postgres.Config{ Conn: sqlDB } )` wrapper.
- Cloud Run/GKE workload identity supplies ADC automatically; **never** ship a downloaded
  service-account key JSON file; reserve `path:` credentials only for **local developer**
  machines that cannot use ADC for some reason.
- Pin versions using `go.mod` and periodically run `cloud-sql-connector` dependency audits
  since certificate chain handling is security-critical.

#### Authentication methods

| Method | When to use | Setup |
|--------|-------------|-------|
| **IAM Database Authentication (default, required for prod)** | All Cloud Run / GKE / GCE workloads | 1. Grant the workload service account `roles/cloudsql.instanceUser` on the instance. 2. Create an IAM database user inside Postgres: `CREATE USER "blog-iam@my-project.iam" WITH LOGIN; GRANT CONNECT ON DATABASE blog TO "blog-iam@my-project.iam"; GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO "blog-iam@my-project.iam"; GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO "blog-iam@my-project.iam";` 3. Pass `cloudsqlconn.WithIAMAuthN()` when creating the Dialer. |
| **Application Default Credentials (local dev)** | Developer laptops connecting to a **dedicated dev** Cloud SQL instance (never prod) | Developer runs `gcloud auth application-default login` once, sets `DB_GOOGLE_CREDENTIALS_SOURCE=adc`; still uses IAM DB auth — no password needed. |
| **Build-in service account key via secrets manager (strict fallback)** | Legacy edge cases; strictly forbidden for prod workload service accounts unless Workload Identity is unavailable | Store the JSON key payload in Secret Manager, reference via `GOOGLE_APPLICATION_CREDENTIALS=/secrets/sa-key.json`; rotate keys every 90 days. Audit with `gcloud asset search-all-iam-policies` periodically. |
| **Built-in database username/password** | **Never in prod**; only local Docker-compose Postgres without cloudsqlconn | Set `DB_HOST=127.0.0.1`, `DB_USER`/`DB_PASSWORD`/`DB_NAME`, `DB_SSLMODE=disable`; the bootstrap layer detects when `DB_INSTANCE_CONNECTION_NAME` is unset and skips the cloudsqlconn Dialer entirely, falling back to direct `libpq`/`pgx`. |

#### Connection pooling best practices

- **Tune `SetMaxOpenConns` to Cloud SQL vCPU count**: a safe upper bound is
  `floor(instance_vcpu * 4)`. For our default `db-custom-2-4096` (2 vCPU) → `MaxOpenConns = 8`
  per service process; 3 app replicas → combined 24 connections (under the Cloud SQL
  PostgreSQL default 100 connection limit).
- **`SetConnMaxLifetime` must be less than Cloud SQL connector cert renewal (1 hour)**. We
  use 30 minutes (1800 seconds). This ensures `database/sql` re-dials at least once per
  certificate window, so old certificate material never lingers in idle connections.
- **`SetConnMaxIdleTime = 10 minutes`**: forces idle connections to be recycled after a
  reasonable idle window so Cloud SQL CPU usage drops during low-traffic periods and we
  pick up any connector configuration changes without a deploy.
- **Use `MaxIdleConns ≤ MaxOpenConns` (typically 40–50% of MaxOpenConns)**. Do NOT set
  `MaxIdleConns` higher than `MaxOpenConns` — Go's `database/sql` will silently cap it,
  but it makes reasoning about metrics harder.
- **Warm the pool during bootstrap**: after creating the `*sql.DB`, call
  `db.SetMaxOpenConns`, `db.SetMaxIdleConns`, `SetConnMaxLifetime`, `SetConnMaxIdleTime`,
  and then immediately open `max_idle` connections using `db.PingContext` N times in parallel
  with per-call 5-second timeouts so the first real request doesn't pay IAM handshake cost.
- **Expose pool metrics to Prometheus**: `db.Stats()` → expose gauges
  `db_pool_open_connections`, `db_pool_in_use_connections`, `db_pool_idle_connections`,
  counters `db_pool_wait_total`, `db_pool_wait_duration_seconds_total`,
  `db_pool_max_idle_closed_total`, `db_pool_max_lifetime_closed_total`. Alert when
  `db_pool_wait_total` rate is non-zero for 5 minutes: it means the pool is saturated.
- **Do NOT use an external pooler** (PgBouncer) alongside `cloudsqlconn` unless
  unavoidable. `cloudsqlconn` already handles certificate rotation and TLS termination;
  adding another layer increases latency and breaks cert refresh timing guarantees.
  If horizontal scaling requires thousands of connections, use Cloud SQL's
  **Connection Manager (built-in PgBouncer)** only for PostgreSQL and keep the cloudsqlconn
  Dialer talking to Connection Manager on port 6432 over private IP.
- **Graceful shutdown**: `*sql.DB.Close()` inside the SIGTERM handler BEFORE stopping Gin;
  this closes pool sockets cleanly, avoiding half-open sockets on the Cloud SQL side which
  would slowly exhaust the instance's connection limit until the watchdog closes them.
- **Cloud SQL connector logging**: set cloudsqlconn debug option `WithDialerLogger(...)` to
  the project's structured logger so certificate refresh, dial timeouts, and private IP
  resolution errors are emitted in the same JSON format as other app logs — never hide
  connector errors behind a failed `sql.Open`.

### Public IP vs Private IP: Scenario, Security, and Verification Comparison

| Dimension | Private IP (production default) | Public IP (dev-only, never prod) |
|-----------|----------------------------------|----------------------------------|
| **Applicable scenarios** | All prod, staging, UAT, and CI/CD long-lived environments. Any workload with VPC-presence (GKE standard/native, GCE, GAE standard, Cloud Run with VPC egress connector enabled). | Short-lived developer Cloud Shell, temporary review deployments that cannot use VPC peering, or local-to-cloud development tunnels over `gcloud sql connect`. |
| **Network path** | Workload VPC → Private Service Connect peering → Google-managed Cloud SQL VPC (public internet never traversed). MTLS end-to-end inside Google network fabric. | Workload → public internet → Google Cloud edge → Cloud SQL SQLproxy / connector-managed MTLS. |
| **IP exposure** | Cloud SQL instance has no public IP at all; no `Authorized Networks` list to manage. Attacker from outside Google's network cannot even resolve the instance IP. | Cloud SQL instance has a public IPv4; must maintain `Authorized Networks` list (0.0.0.0/0 forbidden). Cloud Armor + WAF recommended on top. |
| **cloudsqlconn Dialer option** | `cloudsqlconn.WithPrivateIP()` (required) + `WithIAMAuthN()`. | `cloudsqlconn.WithPublicIP()` (default behaviour when PrivateIP is not requested) + `WithIAMAuthN()`. |
| **Firewall rules** | VPC egress TCP 5432 (PostgreSQL) to `servicenetworking` range only; no ingress needed to the workload subnet since cloudsqlconn always dials outbound. | Workload egress to `35.0.0.0/8` + Google published Cloud SQL ranges TCP 5432; Cloud SQL Authorized Networks must include the workload's NAT egress IP(s). |
| **Cost and latency** | No public IP charge; Private Service Connect has a small hourly charge per peered range. Latency ~1–2 ms inside region. | Public IPv4 hourly charge starting October 2025 GCP pricing; Cloud SQL egress charges. Latency 10–25 ms depending on path. |
| **Security config requirements** | Workload Identity (GKE) / runtime service account + `cloudsql.instanceUser` + IAM DB user; no static credentials. Workload on VPC must have private IP (not public-only GKE autopilot default). Authorized Networks list MUST be empty. | Workload identity still strongly recommended; but it is tempting to add a `DB_PASSWORD` + `Authorized Networks 0.0.0.0/0` — both are audit findings. Enforce policy: `constraints/sql.restrictPublicIp` at org policy level for prod projects. |
| **Connectivity verification (quick)** | 1. SSH/exec to workload pod; run `gcloud sql instances describe blog-pg-01 --format="value(ipAddresses)"` → should only show `PRIVATE` line. 2. `nc -zv <private-ip-of-sql> 5432` → succeeds. 3. From pod that does NOT have IAM binding, `nc` works but IAM login fails with `pg_hba.conf rejects IAM` error, proving network path is open but auth still enforced. | 1. Run `dig blog-pg-01.us-central1.sql.goog` (or use Cloud SQL proxy `hostaddr`). 2. `nc -zv <public-ip> 5432` from a whitelisted source succeeds; from a non-whitelisted source, `timeout`. 3. IAM login verification same as private IP. |
| **Cloud Audit Logs coverage** | `private_protocol_connect` event with `connectionType=PRIVATE` emitted to Data Access audit logs. | `cloudsql.googleapis.com/cloudsql.connect` event with `connectionType=PUBLIC_IP` emitted. |
| **GCP org policy guardrails** | Apply `constraints/sql.restrictPublicIp` and `constraints/sql.requireSsl` constraints on the project/folder to prevent drift. | **Cannot** apply `restrictPublicIp`. Apply `constraints/sql.restrictAuthorizedNetworks` to forbid `0.0.0.0/0`. |

### Troubleshooting guide for common connection failures

#### General approach

1. **Identify the failing layer**: Narrow down the error to `cloudsqlconn dial` vs
   `database/sql login` vs `GORM migration`. The Go errors from `cloudsqlconn` always
   include the canonical error code from `google.golang.org/api/googleapi` or `gRPC codes`.
2. **Always capture the full structured log line**: `app doctor` has a dedicated
   `Cloud SQL connectivity` probe that exercises the full path (dial + login + ping) and
   emits a JSON report with `dialer_error`, `login_error`, `ping_error`, `instance_name`,
   `connection_type`, and `workload_sa`.
3. **Cross-reference with GCP console**:
   - Cloud SQL → instance → **Operations** to see recent restarts / failovers / maintenance windows.
   - Cloud SQL → instance → **Logs** for database-level errors (PostgreSQL `FATAL:` lines).
   - IAM → Policy Analyzer → `cloudsql.instanceUser`, `cloudsql.client` bindings to confirm the workload SA is present.
   - Logs Explorer → query `resource.type="cloudsql_database" AND severity>=ERROR`.

Below are the most common failure modes with step-by-step fixes.

---

#### T1. Private IP network reachability verification (dial timeout, "no such host", "connection refused")

**Symptoms**: errors like:
- `dial tcp <private-ip>:5432: i/o timeout`
- `cloudsqlconn: failed to connect to instance "project:region:instance": context deadline exceeded`
- `failed to discover instance: googleapi: Error 403: The client is not authorized to make this request.` (earlier layer)

**Step-by-step**:

1. **Verify Cloud SQL instance actually has a private IP in the workload region/VPC**:
   ```bash
   gcloud sql instances describe blog-pg-01 --format=yaml | grep -E "ipAddress|type|network"
   ```
   Expect exactly one entry with `type: PRIVATE` and an IP in the `servicenetworking`-allocated
   `/16` range for the peered VPC. If the entry is missing, go to
   <https://console.cloud.google.com/sql/instances/blog-pg-01/edit> → Connections → Private IP →
   select the workload VPC → enable Private Service Connect (this may take 10–20 minutes).
2. **Confirm the workload is attached to that VPC**:
   - GKE: `kubectl describe pod <pod> -n blog | grep -E "hostIP|nodeName"` then verify the node subnet is in the same VPC as Cloud SQL.
   - Cloud Run: confirm **VPC egress connector** is attached and `egress=all-traffic` or at least `private-ranges-only`.
3. **Validate the VPC peering**:
   ```bash
   gcloud compute networks peerings list --network=blog-vpc
   ```
   Look for a peering named `cloudsql-peer-...` with `state: ACTIVE` and `exchangeRoutes: true`.
4. **Basic network probe from inside a workload pod**:
   ```bash
   # PostgreSQL
   nc -zv 10.123.45.67 5432
   ```
   If this times out, check VPC firewall egress rules and the Private Service Connect peering
   firewall. 90% of the time the fix is: add an egress rule `allow-blog-to-cloudsql-private`
   targeting the workload service account with TCP destination port 5432 to the
   `servicenetworking` range.
5. **Confirm the workload uses `cloudsqlconn.WithPrivateIP()`**: set
   `DB_PRIVATE_IP_ENABLED=true`; `app doctor` reports `connection_type: PRIVATE`. If you see
   `connection_type: PUBLIC_IP` in logs with a private Cloud SQL instance, the Dialer will
   never connect because there is no public IP to dial.
6. **Check Google APIs permissions for Dialer**: `cloudsql.instanceUser` (on the instance)
   grants **private IP connects**. If missing → 403 during instance discovery. Add:
   ```bash
   gcloud sql instances add-iam-policy-binding blog-pg-01 \
     --member=serviceAccount:blog-sa@my-project.iam.gserviceaccount.com \
     --role=roles/cloudsql.instanceUser
   ```

#### T2. Database permission configuration (login OK but SQL queries fail with permission denied)

**Symptoms**: connection opens, `db.PingContext` returns nil, but migrations fail with
`pq: permission denied for table schema_migrations`.

**Step-by-step**:

1. **Confirm the IAM database user exists inside the database**:
   ```sql
   -- PostgreSQL
   SELECT rolname FROM pg_roles WHERE rolname = 'blog-iam@my-project.iam';
   ```
   If the row is missing, create the user (see §Authentication → IAM DB Auth).
2. **Confirm schema grants**: the fastest remediation is to re-run the GRANT block:
   ```sql
   -- PostgreSQL
   GRANT CONNECT ON DATABASE blog TO "blog-iam@my-project.iam";
   ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES    TO "blog-iam@my-project.iam";
   ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO "blog-iam@my-project.iam";
   ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON FUNCTIONS TO "blog-iam@my-project.iam";
   ```
   Then re-run the existing `GRANT ALL PRIVILEGES ON ALL …` statements to cover tables created
   *before* the default privileges were altered.
3. **Confirm the table schema owner matches the migration user** — if migrations were
   initially applied by a `blog-superuser` account while app connects as `blog-iam`, the
   tables are owned by `blog-superuser` and default privileges do not backdate. Fix:
   `ALTER TABLE … OWNER TO "blog-iam@my-project.iam";` (or run migrations under the same
   IAM service account that the app uses, which is the recommended practice).
4. **Confirm the IAM service account login matches the DB user exactly**: PostgreSQL IAM DB
   usernames are case-sensitive and must include the `.iam` suffix. The mapping between the
   Google SA `blog-sa@my-project.iam.gserviceaccount.com` and the DB username
   `blog-iam@my-project.iam` is set during `CREATE USER …`; if you accidentally left out
   the `.iam` suffix, delete the DB user and recreate it — otherwise login fails with
   `IAM user not found`.
5. **Review Cloud SQL Data Access audit logs**: for PostgreSQL, enable the `data_access` log
   type on the project; each SQL error will be logged with `user`, `database`, and the raw
   SQL text truncated appropriately — invaluable for distinguishing a `GRANT` bug from
   application SQL syntax bugs.

#### T3. cloudsqlconn credential configuration (ADC failures, Workload Identity, 403/401)

**Symptoms**: errors containing:
- `google: could not find default credentials`
- `oauth2: token expired and refresh token is not set`
- `googleapi: Error 401: Request had invalid authentication credentials`
- `Failed to generate ephemeral certificate: googleapi: Error 403: Request had insufficient authentication scopes`

**Step-by-step**:

1. **Identify which credentials path the Dialer used**: set
   `DB_GOOGLE_CREDENTIALS_SOURCE=workload-identity|adc|path:…` explicitly. `app doctor`
   will include a `credentials_source_resolved` field in its report — this short-circuits 90%
   of confusion between ADC and Workload Identity.
2. **Workload Identity (GKE)**:
   - Verify KSA ↔ GSA binding:
     ```bash
     gcloud iam service-accounts get-iam-policy blog-sa@my-project.iam.gserviceaccount.com \
       --flatten="bindings[].members" --filter="bindings.role:roles/iam.workloadIdentityUser" --format="value(bindings.members)"
     ```
     Must include `serviceAccount:my-project.svc.id.goog[blog/blog-ksa]`.
   - Pod annotation: `kubectl get pod <pod> -o jsonpath='{.metadata.annotations.iam\.gke\.io/gcp-service-account}'`
     → must match `blog-sa@my-project.iam.gserviceaccount.com`.
   - Node pool OIDC scopes include `cloud-platform`. If running on an older node pool with
     a restricted scope list, re-create the node pool.
3. **Application Default Credentials (local dev)**:
   - Run `gcloud auth application-default print-access-token` locally. If this fails → re-run
     `gcloud auth application-default login`.
   - Ensure the quota project is set: `gcloud auth application-default set-quota-project my-project`.
   - Confirm the human account has: `cloudsql.instanceUser` on the instance (private IP),
     `cloudsql.client` on the project, and the matching IAM database user mapping (T2).
4. **Secrets-manager key path**:
   - If `DB_GOOGLE_CREDENTIALS_SOURCE=path:/secrets/sa-key.json`, verify the file exists
     inside the container, is readable (0400) by the app user, and the JSON has valid
     `type: service_account`, `project_id`, `private_key` fields.
   - Rotate any key that has been on disk for more than 90 days; use Workload Identity instead.
5. **Scope issues (Error 403 insufficient authentication scopes)**: the token used by
   `cloudsqlconn` must have the `https://www.googleapis.com/auth/sqlservice.admin` scope
   (which ADC/Workload Identity normally supply by default). If you are running on a
   manually-configured custom token exchange, ensure this scope is requested.
6. **Test pure `cloudsqlconn` in isolation**: write a tiny Go program that only calls
   `dialer.Dial(ctx, DB_INSTANCE_CONNECTION_NAME)` — if this fails, the problem is before
   the database login layer; if it succeeds, move to T2/T4.

#### T4. Certificate rotation / handshake errors (TLS "bad certificate", connection dies ~55 minutes)

**Symptoms**: connections begin dropping after ~55 minutes exactly, logs contain:
- `tls: bad certificate`
- `x509: certificate has expired or is not yet valid`
- `handshake failure` on a long-lived connection right around the 1h mark.

**Step-by-step**:

1. **Confirm `SetConnMaxLifetime < 1h` (30m recommended)**: if `DB_POOL_MAX_LIFETIME_SECONDS`
   is greater than 3600, `database/sql` will hold sockets past the Cloud SQL connector's
   certificate refresh boundary. Set to 1800 — this alone fixes ~80% of "drops every hour" issues.
2. **Ensure workload clock skew is low**: on GKE/GCE, the VM clocks are Google-slewed, but
   custom on-prem VPCs peering in may have drift. Use `chrony` to sync against
   `time.google.com`; if offset > 5 seconds for more than 5 minutes, cert validation will
   have spurious failures.
3. **Check Cloud SQL instance maintenance window**: if the error aligns with a scheduled
   maintenance (visible in Operations → Maintenance updates), the connector will transparently
   re-dial on the next `sql.DB` connection turnover; the issue is self-resolving. If it
   happens mid-request, consider adding retry for the first failure per request with a
   250 ms backoff (idempotent reads only).
4. **Confirm connector version**: versions prior to `cloudsqlconn@v1.4.0` had a rare race
   condition where a new cert was not picked up during a refresh cycle overlap. Keep on the
   latest stable patch version.
5. **Capture the connector debug logs**: use `cloudsqlconn.WithDialerLogger(...)` at debug
   level for one pod during a controlled reproduction; the logs contain
   `certificate.refresh.success` events and `certificate.refresh.failure` with the underlying
   error code. If refresh fails with `quotaExceeded`, the project is hitting the Cloud SQL
   Admin API quota (default 6000 requests per minute per project — very rare, increase the
   quota).

#### T5. Slow connect latency or cold-start hangs (Cloud Run)

**Symptoms**: on Cloud Run, the first request after a cold start takes > 5 seconds before
the first SQL statement runs.

1. **Minimize cold-start latency**: the cloudsqlconn Dialer fetches the instance metadata
   and ephemeral cert on first use; pre-warm it inside `main()` with a `dialer.Dial` call
   before starting the HTTP server on a non-blocking goroutine.
2. **Configure minimum Cloud Run instances**: set `min-instances=2` on prod so 2 JIT-warmed
   containers with already-initialized Dialers and DB pools are always online.
3. **Use Cloud Run CPU always-allocated** (`cpu-throttling: off`) so idle connection
   recycler goroutines and pool refreshes don't get paused between requests.
4. **Confirm the VPC egress connector is co-located in the same region as the Cloud SQL
   instance — cross-region peering introduces 20–80 ms latency per dial which is noticeable
   on cold start.

#### T6. Wrong instance connection name or typo

**Symptom**: `cloudsqlconn: instance connection name "my-project:us-east1:bogus" not found`
or `invalid instance connection name: missing 2nd colon`.

**Fix**: canonical format is `PROJECT_ID:REGION:INSTANCE_NAME` — three components separated by colons.
Use `gcloud sql instances describe blog-pg-01 --format='value(connectionName)'` to copy/paste
the exact value instead of hand-typing; store in environment via Terraform/Infra-as-code export
so it is never written by hand.

### Recommended Fields

### users

- id
- full_name
- email
- email_normalized (unique index; lowercased trimmed for lookups)
- password_hash_version (tinyint: 1=bcrypt, 2=argon2id, future)
- password_hash
- password_changed_at
- require_password_change_next_login boolean default false
- avatar_id (FK -> media_assets.id, nullable, one-to-one avatar)
- avatar_url (computed/legacy fallback or sync-friendly field, optional)
- status enum: active, invited, locked, suspended, soft_deleted
- login_count int
- last_login_at
- last_login_ip (truncated)
- last_login_ua_bucket varchar(64)
- email_verified_at
- email_change_pending_new_email (nullable encrypted or hash)
- email_change_pending_token_jti (nullable)
- created_at
- updated_at
- deleted_at (nullable, for user soft-delete)

### roles

- id
- name
- slug
- description
- created_at
- updated_at

### permissions

- id
- key
- description
- resource
- action
- created_at
- updated_at

### user_roles

- id
- user_id
- role_id
- assigned_by
- created_at

### role_permissions

- id
- role_id
- permission_id
- created_at

### posts

- id
- author_id
- category_id
- title
- slug
- excerpt
- content
- cover_image_media_id (FK -> media_assets.id, nullable, primary cover image)
- cover_image_url (optional derived/fallback field)
- status
- published_at
- created_at
- updated_at
- deleted_at
- search_vector (stored generated tsvector with a GIN index; see [search.md](search.md))

### post_media

- id
- post_id (FK -> posts.id)
- media_asset_id (FK -> media_assets.id)
- kind (cover, inline_image, attachment)
- sort_order
- created_at

### categories

- id
- parent_id
- name
- slug
- description
- image_id (FK -> media_assets.id, nullable, one-to-one category cover image)
- lft
- rgt
- depth
- sort_order
- created_at
- updated_at

### tags

- id
- name
- slug
- created_at
- updated_at

### post_tags

- id
- post_id
- tag_id
- created_at

### comments

- id
- post_id
- parent_id
- author_name
- author_email
- author_website
- content
- status
- lft
- rgt
- depth
- ip_address
- user_agent
- created_at
- updated_at

### media_assets

- id
- parent_id
- storage_key
- original_filename
- content_type
- size_bytes
- width
- height
- checksum_sha256
- disk
- tags
- metadata
- lft
- rgt
- depth
- sort_order
- uploaded_by
- created_at
- updated_at
- deleted_at

### media_transforms

- id
- media_asset_id
- cache_key
- transform_name
- width
- height
- fit
- format
- quality
- storage_key
- size_bytes
- content_type
- last_accessed_at
- expires_at
- created_at
- updated_at

### audit_logs

- id
- actor_id
- action
- resource_type
- resource_id
- request_id
- ip_address
- metadata
- created_at

### outbox_events

- id
- aggregate_type
- aggregate_id
- event_name
- payload
- status
- published_at
- retry_count
- created_at
- updated_at

### settings

- id
- key
- value_jsonb
- value_type
- sensitivity
- category
- version
- description
- updated_by
- created_at
- updated_at

### settings_history

- id
- setting_id
- key
- previous_value_jsonb
- new_value_jsonb
- changed_by
- request_id
- ip_address
- created_at

### impersonation_sessions

- id
- impersonator_user_id
- impersonator_session_id
- impersonated_user_id
- state
- started_at
- expires_at
- exited_at
- revoked_reason
- document_id
- reason
- stepup_verified
- csrf_token_hash
- request_id
- ip_address
- created_at
- updated_at

### post_revisions

Append-only history table for all post revisions (create/update/restore/publish/archive).

- id (bigint identity PK), uuid (unique public id)
- post_id (FK -> posts.id, indexed, CASCADE on post delete)
- revision_number (integer per-post sequence; unique(post_id, revision_number))
- revision_type enum: create, update, restore, publish, archive
- title (snapshot)
- slug (snapshot)
- excerpt (snapshot)
- content (snapshot)
- status (snapshot)
- author_id (user_id of the modifier/editor, indexed)
- category_id_snapshot
- cover_image_media_id_snapshot
- media_snapshot_jsonb (post_media rows snapshot)
- tags_snapshot_jsonb (tag ids + names snapshot)
- seo_snapshot_jsonb (SEO fields snapshot)
- diff_jsonb: per-field old/new (title/slug/excerpt/content/status/category/cover/media/tags/SEO)
- changelog_text: auto-generated human-readable summary
- editor_note: optional free text note at save time
- restore_from_revision_id (nullable FK -> post_revisions.id, indexed)
- impersonator_id (nullable)
- impersonation_session_id (nullable)
- request_id (nullable for correlation)
- created_at
- updated_at

### post_seo

One-to-one SEO configuration for posts. Option A (separate table); fallback Option B is embedding these fields directly on posts.

- id (bigint identity PK), uuid (unique public id)
- post_id (FK -> posts.id, unique, indexed, CASCADE on post delete)
- seo_title
- seo_description
- seo_keywords JSONB (string array)
- og_title
- og_description
- og_image_id (nullable FK -> media_assets.id, SET NULL on media delete)
- og_url (nullable)
- twitter_card enum: summary, summary_large_image, app, player
- twitter_title
- twitter_description
- twitter_image_id (nullable FK -> media_assets.id, SET NULL on media delete)
- twitter_creator
- canonical_url (nullable)
- robots_noindex boolean default false
- robots_nofollow boolean default false
- created_at
- updated_at

### user_profiles

Sparse one-to-one extension to `users` carrying seldom-edited contact/social/locale payload.
Kept separate from `users` so the primary auth row stays lean for login hot-path queries.

- id (bigint identity PK), uuid (unique public id)
- user_id (FK -> users.id, UNIQUE, CASCADE on user delete; mandatory NOT NULL)
- display_name varchar(60) (nullable, application-level case-insensitive unique; non-nulls enforce DB unique partial index)
- bio text (max 4000 chars sanitized markdown)
- encrypted_contact_phone bytea (AES-256-GCM envelope ciphertext for phone number; never stored plain text)
- contact_phone_verified_at nullable timestamp (SMS/OTP verified flag for E.164)
- contact_website varchar(2048) (http/https only)
- contact_location varchar(120)
- social_links JSONB: {twitter, linkedin, github} — normalized to slugs or full URLs
- locale varchar(32) (BCP 47; e.g. en_US, zh_Hans_CN)
- timezone varchar(64) (IANA tz name; default UTC)
- marketing_consent boolean default false
- marketing_consent_updated_at
- updated_by (FK -> users.id, nullable; for admin-initiated updates or impersonation)
- created_at
- updated_at

### user_privacy_settings

One-to-one per user: controls visibility of public profile and related routes.

- id (bigint identity PK), uuid (unique public id)
- user_id (FK -> users.id, UNIQUE, CASCADE)
- visibility_profile enum: public, unlisted, private, followers_only — default public
- visibility_email boolean default false (never show email publicly; admin and owner still see it)
- visibility_contact_details boolean default false (applies to phone/website/location on public page)
- visibility_activity_timeline boolean default false (applies to public `/users/:name/activity` if ever exposed)
- search_allow_indexing boolean default true (controls SSR `X-Robots-Tag` header and `<meta name=robots>`)
- tracking_personalize_ads boolean default false (propagates to analytics consent service)
- updated_by (FK -> users.id, nullable)
- created_at
- updated_at

### user_password_history

Append-only password hash history used by the N-history reuse policy (default N=10).

- id (bigint identity PK), uuid (unique public id)
- user_id (FK -> users.id CASCADE)
- password_hash_version tinyint (1=bcrypt, 2=argon2id, …)
- password_hash text (bcrypt/argon2id output; same hash policy as users.password_hash)
- created_at

### password_reset_tokens

Opaque + JWT reset token tracking table for forgot-password AND email-change tokens.
All tokens are single-use via `consumed_at` and server-side Redis SETNX guard to prevent replay within a window.

- id (bigint identity PK), uuid (unique public id)
- user_id (FK -> users.id CASCADE; nullable when scope=email_change lookup by JTI)
- jti varchar UNIQUE NOT NULL (matches JWT jti claim)
- scope enum: forgot, email_change
- token_sha256 bytea (SHA-256 of opaque bearer token, optional)
- email_address_hash bytea (peppered HMAC of email; used for server-side salt check vs URL parameter)
- expires_at timestamp NOT NULL
- consumed_at nullable timestamp
- issued_ip (truncated; v4 /24, v6 /64 or encrypted bytea depending on policy)
- issued_ua_bucket varchar(64)
- created_at

### user_activity

Append-only engagement trail surfaced via `/me/activity`. This table serves both as the user-visible
engagement history AND provides audit-grade context for security/trust investigations at admin level.

- id (bigint identity PK), uuid (unique public id)
- user_id (FK -> users.id CASCADE, index)
- session_id nullable (JWT jti or opaque session id)
- impersonator_id nullable FK -> users.id
- impersonation_session_id nullable UUID
- request_id nullable (correlates to API gateway/RPC request id)
- activity_type enum: login, logout, profile_edit, avatar_update, password_change, password_reset, email_change, consent_grant, consent_withdraw, post_create, post_edit, post_publish, comment_create, twofa_enable, twofa_disable, oauth_link, oauth_unlink, impersonation_start, impersonation_end, role_change, settings_view, privacy_change, marketing_consent_grant, marketing_consent_withdraw, export_requested, erasure_requested
- summary varchar(500) (human-readable, safe to display; no secrets)
- detail_jsonb (schema-per-type; contains post_id, route, device class, partial geo, diff_hash, etc.)
- ip_address (truncated; v4 /24 or v6 /64; admin see full via encrypted detail_jsonb if needed)
- user_agent_bucket varchar(64)
- geo_country_code char(2) nullable
- geo_subdivision varchar(16) nullable
- created_at timestamp (never updated)

### user_activity_daily (materialized view)

Daily-aggregate materialized view refreshed hourly by the `app scheduler` worker. Used for admin dashboards
and self-service "active days" charts.

- day DATE
- user_id UUID
- login_count int, profile_edit_count int, avatar_update_count int, password_change_count int, post_create_count int, comment_create_count int, twofa_events_count int
- unique_active_minutes smallint (estimate, bucketed)
- created_at timestamp (refresh marker)

UNIQUE PRIMARY KEY (day, user_id)

### casbin_rules

Generic Casbin policy tuple table. Source of truth for **all** RBAC decisions. Format follows the canonical Casbin database adapter (tuple-style columns v0..v5 for extensibility). The ORM/DB adapter maps this table into an in-memory Casbin Model.

- id (bigserial PK or UUID; bigserial preferred for insertion-heavy tuple writes)
- p_type varchar(8) NOT NULL — Casbin policy type: `p` (permission rule), `g` (grouping/user-role assignment), `g2` (role-role inheritance), or future `e` (explicit effect line rarely used)
- v0 varchar(255) — subject; for p_type=`p` → role or user; for p_type=`g` → user_id or `user:<id>`; for p_type=`g2` → child role
- v1 varchar(255) — object (permission key like `rbac.role:read` or `*:*`); or for grouping → role or parent role
- v2 varchar(255) — action/scope (`read` / `manage` / `*`) or for grouping → domain (always `*` unless multi-tenant mode enabled)
- v3 varchar(16)  — effect: `allow` | `deny` (only meaningful when p_type=`p`; grouping rules ignore it)
- v4 varchar(128) — reserved/domain (future workspace/team column)
- v5 varchar(128) — reserved
- created_at timestamp
- updated_at timestamp
- UNIQUE (p_type, v0, v1, v2, v3, v4, v5) — dedup identical policy rows across all columns

### rbac_roles

Role metadata catalog (mirror). Writes are driven by RBACService after Casbin adapter commits; never write to this table directly.

- id (bigint identity PK), uuid (unique public id)
- name varchar(60) UNIQUE — snake_case role name; DB unique index
- display_name varchar(120) — human-friendly name for UI
- description varchar(500)
- tier int2 NOT NULL CHECK (tier BETWEEN 1 AND 4) — 1=viewer tier, 4=superadmin; used for tier gating enforcement
- inherits_from_id UUID nullable FK → rbac_roles.id (self-referential for Casbin `g2` role-role inheritance rules; when a role is saved its inheritance row in casbin_rules is upserted)
- is_system boolean NOT NULL DEFAULT false — true for `viewer|editor|admin|superadmin`; cannot be deleted
- is_custom boolean GENERATED ALWAYS AS (NOT is_system) STORED
- hidden_from_ui boolean DEFAULT false — quarantine/internal roles not exposed in default role pickers
- version int NOT NULL DEFAULT 0 — optimistic lock counter; incremented on every permission or metadata edit
- created_by UUID FK → users.id
- created_at timestamp
- updated_at timestamp
- deleted_at timestamp nullable (soft delete only for custom roles; system rows never soft delete)

### rbac_permissions

Permission registry catalog (mirror). Source of truth is the code-side typed registry + seed. This table is the readable/searchable mirror used by Admin UI list/search.

- id (bigint identity PK), uuid (unique public id)
- key varchar(180) UNIQUE NOT NULL — `resource:action[:scope]`. Max ~180 chars to accommodate 3 colon segments comfortably
- resource varchar(64) NOT NULL — first segment (rbac.role, user, post, settings, impersonation, analytics, …)
- action varchar(64) NOT NULL — second segment (read, manage, update, publish, export, …)
- scope varchar(64) NOT NULL DEFAULT 'all' — optional third segment (all, own, uuid, security, …)
- description varchar(500)
- category varchar(64) NOT NULL (RBAC, User, Post, Media, Settings, Analytics, Impersonation, Profile, System)
- default_tier int2 — default minimum tier a role must have to be granted this when a new role uses "inherit from viewer/editor/admin" template
- hidden_flag boolean NOT NULL DEFAULT false — hide from UI (internal only permissions)
- code_version varchar(32) nullable — last code registry schema version that asserted this key exists (drift detection via doctor)
- created_at timestamp
- updated_at timestamp

### user_role_assignments

Mirror of Casbin `g(user_id, role_id)` grouping tuples. UI-searchable join table with admin metadata (who assigned, when, optional temp grant expiry).

- id (bigint identity PK), uuid (unique public id)
- user_id UUID NOT NULL FK → users.id ON DELETE CASCADE
- role_id UUID NOT NULL FK → rbac_roles.id ON DELETE CASCADE
- assigned_by UUID NOT NULL FK → users.id (required; system assignments = bootstrap superadmin id)
- assignment_reason varchar(500) nullable — free text note from admin
- expires_at timestamp nullable — temporary grant; scheduler sweeper revokes expired rows (sweeper also removes matching Casbin g-row)
- source enum: api, bulk_import, ldap_sync, bootstrap_seed, self_signup_default
- impersonation_session_id UUID nullable — only used when the assignment was made *during* an impersonation session for audit trail (never used for logic)
- created_at timestamp NOT NULL
- updated_at timestamp NOT NULL
- UNIQUE(user_id, role_id) — one role assignment per user+role pair (expires_at changes still use the same pair; update)

### rbac_policy_audit_log

Append-only log of policy mutations. PostgreSQL `RULE` or trigger blocks any UPDATE/DELETE on this table (except superuser curator role).

- id (bigserial PK)
- mutation_type enum: role_created, role_updated, role_deleted,
  role_permissions_assigned, role_permissions_revoked,
  user_role_assigned, user_role_revoked,
  policy_force_reload, mirror_resync_triggered, mirror_drift_repaired
- actor_user_id UUID FK → users.id
- impersonator_id UUID nullable FK → users.id
- impersonation_session_id UUID nullable
- target_role_id UUID nullable
- target_user_id UUID nullable
- tuples_added_jsonb JSONB — array of added Casbin tuples `[{p_type, v0..v5}]`
- tuples_removed_jsonb JSONB — array of removed tuples
- mirror_delta_jsonb JSONB — before/after of rbac_roles / user_role_assignments mirror rows
- tier_escalation_detected boolean DEFAULT false — review flag when mutation attempted to raise tier to equal caller
- request_id UUID nullable
- ip_address inet nullable (truncated for GDPR: /24 or /64)
- user_agent_bucket varchar(64) nullable
- stepup_verified enum: none, password, totp, webauthn — for compliance audit of 2FA proof presence
- created_at timestamp NOT NULL DEFAULT now()

### rbac_enforcement_events

Append-only request-level enforcement audit. Written through the Watermill outbox → async bulk insert worker to avoid adding latency to hot-path requests. Retention: 90 days hot, 1 year cold archive (moved by scheduler).

- id (bigserial PK)
- decision enum: allow, deny, deferred, error
- deny_reason_code varchar(64) nullable — matches error catalogue (rbac.forbidden, rbac.deny_storm, …)
- user_id UUID NOT NULL (resolved authenticated user; 00000000-0000-0000-0000-000000000000 for anonymous → still audited but code paths usually 401 before RBAC)
- impersonator_id UUID nullable
- impersonation_session_id UUID nullable
- effective_roles_array TEXT[] — snapshot of roles at enforce time (for debugging)
- required_object varchar(180) NOT NULL — permission.object / permission string
- required_action varchar(64) NOT NULL
- required_domain varchar(64) NOT NULL DEFAULT '*'
- request_method varchar(8) NOT NULL
- request_path varchar(512) NOT NULL
- route_pattern varchar(256) NOT NULL — e.g. `/api/v1/admin/users/:id/roles` (for grouping)
- request_id UUID nullable
- trace_id varchar(64) nullable
- ip_address inet (truncated)
- user_agent_bucket varchar(64)
- latency_ns bigint NOT NULL — wall-clock time from middleware enter → exit (perf SLO tracking)
- enforcer_generation_id bigint NOT NULL — cache generation at time of decision (helps debug cache stale issues after reload)
- created_at timestamp NOT NULL DEFAULT now()
PARTITION KEY (monthly list partitioning on created_at recommended for retention drops)

## Notes

- use UUIDs for all primary keys
- add unique indexes for `users.email`, `users.email_normalized`, `roles.slug`, `permissions.key`, `posts.slug`, `categories.slug`, `tags.slug`, `settings.key`, `user_profiles.display_name` (partial unique where not null), `user_profiles.user_id`, `user_privacy_settings.user_id`, `password_reset_tokens.jti`, `rbac_roles.name`, `rbac_permissions.key`, `user_role_assignments(user_id, role_id)`, `casbin_rules(p_type, v0, v1, v2, v3, v4, v5)`
- index foreign keys and frequently filtered status columns
- use JSON or JSONB for flexible metadata, event payload, and setting value fields where appropriate
- model `categories`, `comments`, and `media_assets` as nested set trees using `parent_id`, `lft`, `rgt`, and `depth`
- add composite indexes for nested set traversal, such as `(lft, rgt)` and where useful `(parent_id, sort_order)`
- add `settings_history(setting_id, created_at desc)` index for efficient change-history lookups
- add composite indexes on `impersonation_sessions`:
  - `(impersonator_user_id, state)` partial for state='active' to enforce one active impersonation per superadmin
  - `(expires_at, state)` for expiry sweepers
  - `(impersonated_user_id, created_at desc)` for audit lookups
- add indexes and FK constraints for media associations:
  - `users.avatar_id` -> `media_assets.id` with ON DELETE SET NULL
  - `categories.image_id` -> `media_assets.id` with ON DELETE SET NULL
  - `posts.cover_image_media_id` -> `media_assets.id` with ON DELETE SET NULL
  - `post_media.post_id` + `post_media.media_asset_id` with composite unique on `(post_id, media_asset_id, kind)` where applicable, plus index `(post_id, sort_order)`
- relational mapping summary:
  - `users (1) -- (0..1) media_assets` via `avatar_id` (one-to-one avatar)
  - `posts (1) -- (0..N) post_media -- (1) media_assets` via join table (one-to-many attached images with kind + order)
  - `categories (1) -- (0..1) media_assets` via `image_id` (one-to-one category cover)
  - `posts (1) -- (0..N) post_revisions` via `post_id` (append-only revisions; CASCADE delete on post delete)
  - `posts (1) -- (0..1) post_seo` via `post_id` (one-to-one SEO config; CASCADE delete on post delete)
  - `post_seo.og_image_id / twitter_image_id` SET NULL on referenced `media_assets` delete
  - `users (1) -- (0..1) user_profiles` via `user_id` (1:1 extension; created lazily on first profile edit or eagerly by seeder; CASCADE delete)
  - `users (1) -- (0..1) user_privacy_settings` via `user_id` (1:1; default row created by user-create trigger or service; CASCADE delete)
  - `users (1) -- (0..N) user_password_history` via `user_id` (append only; CASCADE delete)
  - `users (1) -- (0..N) password_reset_tokens` via `user_id` (append-only; CASCADE delete; email_change tokens still carry user_id too)
  - `users (1) -- (0..N) user_activity` via `user_id` (append-only engagement log; CASCADE delete on hard user deletes activity)
  - `users (1) -- (0..N) user_activity` via `impersonator_id` SET NULL when the impersonator account is removed from the system (soft delete or CASCADE via impersonator_id FK)
  - `users.avatar_id` SET NULL on referenced `media_assets` delete; detaching an avatar never deletes the user row
- add composite indexes on `post_revisions`:
  - `(post_id, revision_number)` unique for friendly per-post numbering
  - `(post_id, created_at desc)` for timeline queries
  - `(author_id)` for editor-centric audit views
  - `(restore_from_revision_id)` partial where not null for restore-audit chains
- add indexes on `post_seo`:
  - `unique(post_id)` to enforce one-to-one
  - `(og_image_id)` and `(twitter_image_id)` for FK joins when resolving social card images
- `post_revisions` rows are append-only and never updated after insert (except for `updated_at` as an implementation detail of ORM defaults); consider triggers or application logic to enforce immutability
- on slug change via SEO endpoints, emit a `blog.post.slug_changed` event so redirect/routing systems can record the old → new mapping
- add indexes on `user_profiles`:
  - `unique(user_id)` to enforce one-to-one
  - partial `unique(display_name)` where `display_name is not null`
  - `(locale)` and `(timezone)` for cohort/analytics queries
- add indexes on `user_privacy_settings`:
  - `unique(user_id)` to enforce one-to-one
  - `(visibility_profile)` to support fast "find all public users" admin searches
- add indexes on `user_password_history`:
  - `(user_id, created_at desc)` — last N passwords scan for the reuse policy
- add indexes on `password_reset_tokens`:
  - `unique(jti)` — JWT/opaque single-use
  - `(user_id, created_at desc)` — recent tokens
  - `(scope, expires_at)` partial where `consumed_at is null` — cleanup sweepers
- add indexes on `user_activity`:
  - `(user_id, created_at desc)` — owner timeline
  - `(activity_type, created_at desc)` — audit by type
  - `(impersonation_session_id, created_at)` — impersonation audit trails
  - GIN index on `detail_jsonb` — admin post_id/route lookups
- `user_activity_daily` materialized view refresh concurrently where supported, coordinated by the `app scheduler` worker with advisory locks so horizontal deployments don't double-refresh
- `user_activity` rows are append-only; never update a row once created. Erasure requests use a separate erase marker and zero out PII-bearing fields (ip_address, detail_jsonb with partial geo, user_agent_bucket) in place or move to a redacted archive table for the retention window — never fully delete while an active investigation may require the record
- encrypted contact fields (user_profiles.encrypted_contact_phone) use envelope encryption with per-row data keys; the KMS/ENV master key MUST NOT appear in migrations, seeds, or code comments; rotation strategy documented in `docs/architecture/security.md`
- user emails are case-insensitive unique via `users.email_normalized` (lowercased, trimmed, punycode-IDNA encoded); the `email` display field preserves original casing

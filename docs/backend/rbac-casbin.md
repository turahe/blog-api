# Backend Module: RBAC with Casbin

## Hexagonal Placement

RBAC module lives under `internal/core/rbac/{domain,ports,service}` with inbound adapters for Gin middleware + admin HTTP handlers, and outbound adapters for: Casbin PostgreSQL adapter (GORM / custom sqlx), Redis watcher pub/sub, audit/append-only repositories, and the permission registry seeder.

```
internal/core/rbac
├── domain/
│   ├── rbac_domain.go              # Role, Permission, PolicyTuple, RoleTier (smallint 1..4)
│   │                                 # EnforcementDecision enum (allow/deny/deferred), PolicyDiff
│   └── values.go                    # Resource/Action/Scope typed strings; registry; matchers (keyMatch3, scopeMatch)
├── ports/
│   ├── rbac_reader_port.go          # ListRoles, GetRole, GetPermissionsCatalog, GetRoleAssignments
│   │                                 # GetUserRoles(user_id), GetImplicitPermissions(user_id)
│   │                                 # BatchEnforce([]EnforceInput) → []bool
│   │                                 # CheckMirrorConsistency → Report
│   │                                 # LookupEnforcementAudit(filter, page)
│   ├── rbac_writer_port.go          # CreateRole, UpdateRole, DeleteRole, SetRolePermissions
│   │                                 # AssignUserRoles, RevokeUserRoles, ReloadPolicyFromDB
│   │                                 # SeedSystemRolesAndRegistry
│   ├── casbin_adapter_port.go       # Outbound: LoadPolicy/SavePolicy/AddPolicy/RemovePolicy (Casbin DB adapter interface)
│   ├── casbin_enforcer_factory_port.go # Outbound: NewSyncedEnforcer(model, adapter, watcher) → ManagedEnforcer
│   ├── policy_watcher_port.go       # Outbound: PublishPolicyUpdated(signed_payload); Subscribe(callback); Redis impl default
│   ├── policy_audit_repository.go   # Outbound: AppendPolicyAudit(diff, actor, request_id)
│   ├── enforcement_audit_repository.go # Outbound: AppendEnforcement(event) — append-only
│   ├── permission_registry_port.go  # Outbound: Code-side seeded registry → upsert into rbac_permissions
│   ├── rbac_cache_port.go           # Outbound: LRU cache (enforce_input → decision, TTL 30s or watcher-invalidate)
│   └── mirror_repository_port.go    # Outbound: Upsert role/permission/user-role mirror rows (rbac_roles, rbac_permissions, user_role_assignments)
└── service/
    └── rbac_service.go              # composes all ports; implements Hex inbound ports
```

Inbound adapters:

```
internal/adapters/inbound/rbac
├── gin_rbac_middleware.go           # Enforce(user, route_permission) → 403 rbac.forbidden or pass;
│                                     # writes enforcement audit; short-circuits impersonation target roles
├── admin_rbac_http_handler.go       # /admin/roles, /admin/users/:id/roles, /admin/permissions, /admin/policies/reload, /admin/audit/access
└── cli_rbac_doctor.go               # `app doctor --rbac-registry | --rbac-model-hash | --rbac-mirror-consistency | --rbac-smoke`
```

Outbound adapters:

```
internal/adapters/outbound/rbac
├── casbin_postgres_adapter.go       # implements casbin persist.Adapter; reads/writes casbin_rules table
├── casbin_batch_adapter.go          # optional persist.BatchAdapter for mass LoadIncrementalFilteredPolicy
├── redis_policy_watcher.go          # Subscribe → HMAC-verify → debounced LoadPolicy(1x/200ms per process)
│                                     # Publish → HMAC-sign → Redis PUBLISH casbin.policy.updated
├── policy_audit_sqlx.go             # INSERT rbac_policy_audit_log (append-only; trigger blocks UPDATE/DELETE)
├── enforcement_audit_sqlx.go        # INSERT rbac_enforcement_events (append-only)
├── permission_registry_embed.go     # embed registry table → seed on app boot; compare DB for orphan detection
├── casbin_model_conf_embed.go       # go:embed model.conf → bytes.Reader → NewModelFromReader; never disk-writable
├── mirror_sync_sqlx.go              # sync Casbin tuples → rbac_roles / rbac_permissions / user_role_assignments
└── rbac_lru_cache.go                # golang-lru or Ristretto: 10k default, TTL 30s, watcher-triggered invalidation
```

## Storage Model (tables summary)

Canonical column-level definitions live in [database.md](./database.md). Summary:

| Table                          | Purpose                                                                                         | Source of truth |
|--------------------------------|-------------------------------------------------------------------------------------------------|-----------------|
| `casbin_rules`                 | Generic Casbin policy tuples (p_type, v0..v5). All enforce decisions read from here via adapter | YES             |
| `rbac_roles`                   | Role metadata (name, tier, description, is_system, version, created_by) — UI searchable catalog | MIRROR          |
| `rbac_permissions`             | Permission registry (key, resource, action, scope, description, hidden_flag)                   | MIRROR + registry code |
| `user_role_assignments`        | users.id ↔ rbac_roles.id, assigned_by, expires_at (nullable temporary grants)                  | MIRROR          |
| `rbac_policy_audit_log`        | Before/after diffs of policy mutations; append-only via PG trigger                              | YES audit only  |
| `rbac_enforcement_events`      | Per-request allow/deny events — streamed to SIEM, queried via admin access audit               | YES audit only  |

Always write to `casbin_rules` first, then mirror tables in the same DB transaction. Never mutate mirror tables independently. A nightly `app doctor --rbac-mirror-consistency` detects drift and re-syncs **Casbin → mirror** unidirectionally.

## Casbin Model & Request / Policy Syntax

Model lives in embedded `model.conf` (RBAC with deny override, optional domain column). Example:

```
[request_definition]
r = sub, obj, act, dom

[policy_definition]
p = sub, obj, act, eft, dom

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub, r.dom) && keyMatch3(r.obj, p.obj) && scopeMatch(r.act, p.act) && r.dom == p.dom
```

Notes:
- `sub` = user_id or role_id (both namespace-collision-free via `user:` / `role:` prefixes stored in v0 of casbin_rules; or simply use RBAC groupings properly)
- `obj` = permission key string or wildcard `post:*`, `*:*`
- `act` = action or scope; the `scopeMatch` function normalizes "own/all/uuid" style scopes
- `eft` = `allow` or `deny`
- `dom` = always `*` for current single-tenant install; reserved for future multi-team workspaces

Custom matchers registered via `enforcer.AddFunction("scopeMatch", ScopeMatchFunc)` and `keyMatch3` built-in.

## Endpoints Catalog

Full HTTP definitions → [api.md](./api.md) + [openapi.yaml](../swagger.json). Summary:

### Admin RBAC
| Method | Path                                               | Permission                  | Description                                                     |
|--------|----------------------------------------------------|-----------------------------|-----------------------------------------------------------------|
| GET    | `/api/v1/admin/roles`                              | `rbac.role:read`            | List roles, paginated, filter by tier/is_system, include counts |
| POST   | `/api/v1/admin/roles`                              | `rbac.role:manage`          | Create custom role (with tier gating)                           |
| GET    | `/api/v1/admin/roles/{idOrName}`                   | `rbac.role:read`            | Role detail + inherited roles + assigned users preview          |
| PATCH  | `/api/v1/admin/roles/{idOrName}`                   | `rbac.role:manage`          | Update metadata/tier/inherits_from (tier gating + 2FA)         |
| DELETE | `/api/v1/admin/roles/{idOrName}`                   | `rbac.role:manage`          | Delete custom role (rejects system roles; cascades assignments) |
| GET    | `/api/v1/admin/roles/{idOrName}/permissions`       | `rbac.role:read`            | Read permissions matrix for a role                              |
| PUT    | `/api/v1/admin/roles/{idOrName}/permissions`       | `rbac.role:manage`          | Overwrite role permission set; body add/remove arrays; optimistic lock; 2FA required for tier3+ writes |
| GET    | `/api/v1/admin/users/{id}/roles`                   | `user.profile:read`         | List roles assigned to a user                                   |
| POST   | `/api/v1/admin/users/{id}/roles`                   | `rbac.role:manage`          | Assign/add user roles; tier gating; optional expires_at         |
| DELETE | `/api/v1/admin/users/{id}/roles/{roleId}`          | `rbac.role:manage`          | Revoke a role from a user; 2FA if revoking admin tier or above |
| GET    | `/api/v1/admin/permissions`                        | `rbac.permission:read`      | Catalog of all registered permissions (registry)                |
| GET    | `/api/v1/admin/policies/consistency`               | `rbac.role:manage`          | Casbin ↔ mirror consistency report (doctor endpoint surfaced)  |
| POST   | `/api/v1/admin/policies/reload`                    | `rbac.policy:reload`        | Force `LoadPolicy()` across this instance + publish via watcher to all replicas; 2FA + superadmin |
| GET    | `/api/v1/admin/audit/access`                       | `rbac.audit:read`           | Paginated enforcement events (allow/deny); CSV export via `?export=csv` |
| POST   | `/api/v1/admin/audit/access/export`                | `rbac.audit:read`           | Async CSV export job (huge windows)                             |

### Self-Service (none)
Self-service endpoints use the middleware. No RBAC-specific self-service endpoints exist except `/me/activity` showing `rbac_*` activity types.

## Validation Rules

- Role name: `^[a-z][a-z0-9_]{1,59}$` (snake_case, max 60 chars). Unique including case-insensitive partial index.
- Permission key registry enforcement: UI must reject permission keys that are not registered (not in code, not in `rbac_permissions`).
- Tier range `1..4` only.
- Inheritance graph must be a DAG of max depth ≤ 6; cycle detection on update returns `rbac.role.inheritance_cycle`.
- `expires_at` on user-role assignments: must be in the future if set.
- Custom matchers scope: regex `^(all|own|[0-9a-fA-F-]{36})$` or extendable per-resource-domain.
- `DELETE` of a system role (`is_system=true`) → always 403 `rbac.role.cannot_delete_system_role`.
- `POST /admin/policies/reload` throttled per-admin + `X-2FA-Verified` header.

## Events (Transactional Outbox)

Complete catalogue → [events.md](./events.md). Summary list:
- `rbac.role.created`, `rbac.role.updated`, `rbac.role.deleted`
- `rbac.role.permissions.updated` (diff summary: added_keys, removed_keys, unchanged_keys)
- `rbac.user.role.assigned`, `rbac.user.role.revoked`
- `rbac.policy.reload_triggered` (with requester + nodes notified count)
- `rbac.access.denied` (fan-out to SIEM + admin SSE live feed; PII-safe fields only)
- `rbac.mirror.drift_detected` (doctor check emits alert event only)

All events inherit `base_event` (event_id, event_name, occurred_at, aggregate_id=role_id/user_id, actor_id, impersonator_id, request_id).

## Caching Strategy

Two tiers:

1. **Enforcer internal cache** (Casbin `SyncedEnforcer` with `EnableCache(true)`): in-memory model-cache; invalidated automatically on adapter writes *plus* watcher subscription.
2. **RBAC LRU cache** (`Ristretto` or lru/v2): cache key = `sha256(sub + "|" + obj + "|" + act + "|" + dom + "|" + enforcer_generation_id)`. Cache size default 10k, TTL 30s. Invalidated globally within 200 ms of any policy write via Redis watcher → process bumps generation_id (a `uint64` atomic counter in-process).

Cache hit rate SLO: ≥99.9%. Metric `rbac_enforce_cache_hit_ratio` exported to Prometheus.

## Rate Limiting Tier Table (RBAC endpoints)

| Endpoint                               | Tier    | Limit                                 | Scope                      |
|----------------------------------------|---------|---------------------------------------|----------------------------|
| POST /admin/policies/reload            | highest | 1/min / admin, 60/min / cluster       | admin_id + instance        |
| Role writes (create/update/delete)     | high    | 30/min / admin                        | admin_id                   |
| Role permissions PUT                   | high    | 30/min / admin per role               | admin_id + role_id         |
| User-role assign/revoke                | med     | 30/min / admin per user               | admin_id + target_user_id  |
| Roles / Permissions reads              | low     | 120/min / admin; cache TTL 30s        | admin_id                   |
| Access audit read                      | med     | 60/min / admin                        | admin_id                   |
| Access audit CSV export                | high    | 2/h / admin; async worker queue       | admin_id                   |
| Enforcement attempt denials storm      | highest | 10/min / user → 429 `rbac.deny_storm` | user_id + ip               |

## Audit Rules

1. Every HTTP request on a protected endpoint → append 1 `rbac_enforcement_events` row (or outbox record) *regardless of decision*. High-volume ingestion path: async channel through Watermill outbox → bulk insert worker (minimize per-request latency).
2. Every policy mutation (role create/edit/delete, permission assign/revoke, user role assign/revoke, reload): 1 `rbac_policy_audit_log` row *inside the same txn* with before/after JSONB diff, actor_id, impersonator_id, request_id, client_ip, tier_escalation_flag boolean.
3. Deny decisions additionally emit `rbac.access.denied` outbox event for near-real-time admin dashboard + SIEM.
4. Audit logs are never returned to non-`rbac.audit:read` callers; redacted by default for lower-tier admins.
5. Retention: `rbac_enforcement_events` 90 days hot + 1 year cold archive; `rbac_policy_audit_log` indefinite.

## Unit / Service / Repo / Integration Tests Expectations

### Unit
- Model parse → valid; matcher functions: keyMatch3 + scopeMatch (10 cases)
- Inheritance transitivity (viewer→editor→admin→superadmin)
- Deny overrides allow (6 cases)
- Tier gating (5 combinations including tier equal → block)
- LRU cache hit latency ≤ 50 μs (benchmark subtest)
- BatchEnforce 100 inputs ≤ 1 ms

### Service
- RBACService.CreateRole fails correctly if tier invalid / name taken / cycle in inherits
- RBACService.AssignUserRoles correctly enforces tier ceiling
- RBACService.ReloadPolicy triggers LoadPolicy + publish + audit
- RBACService.DoctorMirrorConsistency flags simulated drift

### Repo
- Casbin adapter roundtrip: AddPolicy → LoadPolicy returns the same set (12 combos)
- Mirror sync inserts + updates correctly on AddPolicy / RemovePolicy
- EnforcementEvent append + paginated read pagination correct

### Integration
- Middleware 200/403 behavior for 78 ops × 4 roles matrix (combinatorial; 312 test cases; use table tests).
- Hot reload across processes A/B using in-process fake Redis pubsub.
- 10k enforcement requests/s → p99 latency < 250 μs with warm cache; DB watcher down → latency p99 < 3 ms.
- Audit log completeness: every HTTP enforcement produces 1 row in a 500-request randomized integration run.
- Custom matcher + model hash unchanged via doctor.

## Hex Dependency Guardrails

1. **RBAC Service → depends only on outbound ports** (never GORM, never *gin.Context, never Redis client directly). Adapters wrap concrete implementations.
2. RBAC middleware inbound adapter is the ONLY place that calls `Enforce` during request pipeline. Controllers must NOT re-check permissions via direct DB queries (duplicate logic = inconsistency risk). Controllers MAY call `BatchEnforce` to display page-level action availability UI badges (read-only).
3. `mirror_repository_port` writes are always invoked after Casbin adapter writes, same txn. Reverse direction is forbidden.
4. `casbin_model_conf_embed` adapter must never accept file writes or HTTP-supplied model bytes.
5. `policy_watcher_port` is the ONLY allowed mechanism for cross-instance invalidation. Direct DB polling capped at 10 s fallback for Redis-down degraded mode, never default.
6. Registry code is the source of truth for new permission keys. Permission keys present in DB but absent in code → `app doctor --rbac-registry` reports as orphan; deployment gating blocks the rollout until orphan is either deleted or re-added to the registry (fail-closed to avoid drift).
7. RBAC service never calls `AuthService` or `ImpersonationService` directly. Instead, middleware resolves `sub=user_id` (or impersonated user id) *before* calling RBAC ports and passes it. Decoupling keeps RBAC pure and testable.
8. JWT tokens carry only roles for UI/logging; the actual permission set is resolved via Enforcer. No trust to JWT permission claims.
9. Outbox events (rbac.*) are published inside the same DB transaction as policy writes to guarantee consistency. Failure to write outbox aborts the transaction.

## Cross-References

- [rbac-with-casbin.md](../features/rbac-with-casbin.md) — Feature level scope, tier matrix, UI spec, staging/UAT
- [database.md](./database.md) — `casbin_rules`, `rbac_roles`, `rbac_permissions`, `user_role_assignments`, audit tables, index details
- [api.md](./api.md) — Admin RBAC endpoints rules + middleware position
- [services.md](./services.md) — Core services list including `RBACService`, `CasbinEnforcerFactory`, etc.
- [events.md](./events.md) — Full event catalog + consumers
- [security.md](../architecture/security.md) — RBAC hardening rules, impersonation inheritance strictness
- [openapi.yaml](../swagger.json) — Contract (paths/schemas/security)
- [asyncapi.yaml](../architecture/asyncapi.yaml) — Event channels & messages

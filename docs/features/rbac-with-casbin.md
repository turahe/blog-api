# RBAC with Casbin

## Feature Summary

Implement a hierarchical, dynamically managed Role-Based Access Control layer backed by the Casbin enforcer (RBAC model with inheritance + domain support + explicit deny overrides), persisted to the existing PostgreSQL datastore via the Casbin DB Adapter, synchronized across instances through a Redis watcher, and enforced on every non-health HTTP endpoint via Gin middleware. The design preserves backward compatibility with the existing `roles` / `permissions` / `user_roles` / `role_permissions` relational tables by mirroring them as readable metadata catalogs while delegating all enforce decisions to Casbin. Casbin is the source of truth for allow/deny decisions; the relational mirror is the UI/search/admin surface.

Sprint E scope implements all 10 requirements:

1. Casbin setup with RBAC model + policy files;
2. Core role hierarchy (viewer → editor → admin → superadmin) with inheritance;
3. Auto-enforcing middleware on all protected endpoints;
4. Management UI + APIs for dynamic role/permission/policy updates (runtime, no restart);
5. Database persistence with PostgreSQL Casbin Adapter, not flat files;
6. User ↔ role assignment endpoints with audit + tier gating;
7. Comprehensive unit + integration tests covering every role × permission pair plus cross-role escalation edge cases;
8. Audit of every enforcement attempt (allow + deny) for SIEM ingestion;
9. Enforcer performance optimization: Redis-watcher, SyncedEnforcer, in-memory policy cache, batch enforcement API, and warm-up hooks;
10. Documentation of role hierarchy, permission matrix, and extension playbook.

## In Scope

- Casbin model v2: RBAC with role inheritance (`g`), user ↔ role assignment (`g`), role ↔ permission assignment (`p`), explicit `deny` overrides via effect priority, optional `domain` column for future multi-tenant/team scopes.
- 4 system roles: `superadmin`, `admin`, `editor`, `viewer`, plus dynamic custom roles created through management APIs.
- Inheritance chain: `viewer < editor < admin < superadmin`; `superadmin` has an allow-all wildcard policy (`*, *`) at the top tier.
- Permission notation: `resource:action[:scope]` (3 segments, colon-delimited); e.g. `post:publish`, `user.profile:edit:own`, `settings:update:security`, `rbac.role:manage`, `analytics:export`.
- Automatic middleware: `auth → impersonation → csrf → rbac-enforce → rate limit → step-up → audit`. Routes are grouped by tag and permission in the OpenAPI contract; middleware resolves the required permission string from the route metadata and calls `Enforcer.Enforce(user, permission)`.
- Management UI pages: Roles list (CRUD), Role detail (members + permissions matrix), Permissions catalog (read-only registry + descriptions), Access Audit viewer.
- Runtime changes to policies invalidate all replicas within 200 ms via Redis watcher publish (CASBIN_POLICY_UPDATED channel); enforcers subscribe and call `LoadIncrementalFilteredPolicy` or full `LoadPolicy` on debounced coalesce.
- Persistence: `casbin_rules` table (p_type, v0..v5 generic tuple per Casbin adapter convention) is the policy source of truth; the existing `roles`/`permissions`/`user_roles`/`role_permissions` tables are kept in sync bidirectionally via RBACService write-side orchestration so search/admin JOINs remain SQL-friendly; writes always go to Casbin adapter first, then mirror to relational tables in the same DB txn.
- User-role assignment endpoints: Admin `POST /admin/users/:id/roles` with tier gating (cannot assign a role at or above the caller's own tier without superadmin override).
- Unit tests: model.conf parsing, policy seed, role inheritance transitivity, wildcard expansion, deny-overrides-allow, tier escalation prevention, Redis watcher reload.
- Integration tests: middleware 403 when missing permission, 200 when granted, impersonation session uses target-user roles only, policy hot reload on two processes side-by-side, audit log coverage for every request.
- Access audit log: every enforcement attempt writes a lightweight `rbac_enforcement_event` row (or emits an append-only outbox event) with decision, user_id, roles, required_permission, path, method, request_id, ip_address, impersonator_id, latency_ns. Failed decisions additionally fan out to `rbac.access.denied` Watermill/SSE channel for admin SIEM.
- Performance optimizations: `SyncedEnforcer` (thread-safe), policy cache LRU 10k entries default, `BatchEnforce` for bulk permission display, pre-warm on process boot with `app serve` hook, `LoadFilteredPolicy` for partial loads on cold-start, deny-fast short-circuit in middleware before heavy service work.
- Documentation: role hierarchy DAG, permission matrix (roles × resources × actions), step-by-step for adding a new role, adding a new resource permission, and onboarding a new protected endpoint.

## Out of Scope

- Full Attribute-Based Access Control (ABAC) policies; Casbin model remains RBAC-only plus optional string-scoped actions. ABAC extension is documented as a future upgrade path using model replacement + attribute adapter.
- Tenant/domain isolation feature flag defaults to disabled; column v5 (domain) is reserved but always `*` for now.
- Flat file policy storage on disk; use DB adapter exclusively.

## User Roles

| Role          | Tier | Inherits from | System role | Deletable | Notes                                                          |
|---------------|------|---------------|-------------|-----------|----------------------------------------------------------------|
| `superadmin`  | 4    | admin         | yes         | no        | wildcard allow `*:*`; bootstrapped only via env + seed; cannot be revoked from self |
| `admin`       | 3    | editor        | yes         | no        | all CRUD on posts/users/media/settings/analytics/impersonation |
| `editor`      | 2    | viewer        | yes         | no        | create/edit own posts, publish own, moderate comments        |
| `viewer`      | 1    | —             | yes         | no        | read public + draft preview own                                |
| Custom roles  | 1-3  | any of 1-3    | no          | yes       | defined via UI; tier ceiling cannot exceed caller's tier      |

Tier is a smallint (1..4) assigned to every role; enforcement of "cannot promote to higher or equal tier than self" uses numeric comparison before policy write.

## Permissions (Registry)

Permissions are **registered** in code as a typed registry (seeded into `rbac_permissions` table on `app seed`) with a stable string key, human description, resource, action, and default-scoping rules. Registration is the only source of new permissions; deleting a permission record is prohibited unless the code registration is also removed (deployment check via `app doctor --rbac-registry`).

### Sample permission registry

| Key                                    | Resource         | Action      | Scope            | Granted by default to       |
|----------------------------------------|------------------|-------------|------------------|-----------------------------|
| `rbac.role:read`                       | rbac.role        | read        | all              | admin, superadmin           |
| `rbac.role:manage`                     | rbac.role        | manage      | all              | superadmin only              |
| `rbac.permission:read`                 | rbac.permission  | read        | all              | admin, superadmin           |
| `rbac.policy:reload`                   | rbac.policy      | reload      | instance         | superadmin only              |
| `rbac.audit:read`                      | rbac.audit       | read        | all              | admin, superadmin           |
| `role.manage`                          | role             | manage      | all              | admin, superadmin (legacy alias for rbac.role:manage) |
| `role.read`                            | role             | read        | all              | editor+                      |
| `user.read`                            | user             | read        | all              | editor+                      |
| `user.create`                          | user             | create      | all              | admin+                       |
| `user.update`                          | user             | update      | all              | admin+                       |
| `user.delete`                          | user             | delete      | all              | superadmin only              |
| `user.profile.read` (cross-user)       | user.profile     | read        | all              | admin+                       |
| `user.profile.edit` (cross-user)       | user.profile     | edit        | all              | admin+                       |
| `user.password.admin_reset`            | user.password    | admin_reset | all              | admin+                       |
| `user.activity.read_all`               | user.activity    | read_all    | all              | admin+                       |
| `me.profile:read`                      | me.profile       | read        | own              | all authenticated users      |
| `me.profile:edit`                      | me.profile       | edit        | own              | all authenticated users      |
| `me.password:change`                   | me.password      | change      | own              | all authenticated users      |
| `me.privacy:read`                      | me.privacy       | read        | own              | all authenticated users      |
| `me.privacy:edit`                      | me.privacy       | edit        | own              | all authenticated users      |
| `me.activity:read`                     | me.activity      | read        | own              | all authenticated users      |
| `me.avatar:upload`                     | me.avatar        | upload      | own              | all authenticated users      |
| `me.avatar:delete`                     | me.avatar        | delete      | own              | all authenticated users      |
| `post:read`                            | post             | read        | all              | viewer+                      |
| `post:create`                          | post             | create      | own              | editor+                      |
| `post:edit`                            | post             | edit        | own (editor) / all (admin) | editor, admin, superadmin |
| `post:publish`                         | post             | publish     | own (editor) / all (admin) | editor+ |
| `post:delete`                          | post             | delete      | all (admin)      | admin+                       |
| `post.revision:read`                   | post.revision    | read        | all              | editor+                      |
| `post.revision:restore`                | post.revision    | restore     | own (editor) / all (admin) | editor+ |
| `post.seo:read`                        | post.seo         | read        | all              | editor+                      |
| `post.seo:update`                      | post.seo         | update      | own (editor) / all (admin) | editor+ |
| `media:read`                           | media            | read        | all              | viewer+                      |
| `media:upload`                         | media            | upload      | all              | editor+                      |
| `media:delete`                         | media            | delete      | all              | admin+                       |
| `media.tag:manage`                     | media.tag        | manage      | all              | admin+                       |
| `settings.read`                        | settings         | read        | all              | admin+                       |
| `settings.update`                      | settings         | update      | all              | admin+                       |
| `settings.history.read`                | settings.history | read        | all              | admin+                       |
| `settings.storage.read`                | settings.storage | read        | all              | superadmin                   |
| `settings.storage.update`              | settings.storage | update      | all              | superadmin                   |
| `settings.smtp.read`                   | settings.smtp    | read        | all              | superadmin                   |
| `settings.smtp.update`                 | settings.smtp    | update      | all              | superadmin                   |
| `impersonation.start`                  | impersonation    | start       | all              | superadmin                   |
| `impersonation.stop`                   | impersonation    | stop        | all              | superadmin                   |
| `impersonation.audit.read`             | impersonation.audit| read      | all              | superadmin                   |
| `analytics.read`                       | analytics        | read        | all              | admin+                       |
| `analytics.export`                     | analytics        | export      | all              | admin+                       |
| `analytics.search.read`                | analytics.search | read        | all              | admin+                       |
| `analytics.realtime.read`              | analytics.realtime| read       | all              | admin+                       |

## Access & Security Rules

1. **Deny by default**: middleware enforces on all routes except `/health/*`; OpenAPI `security: []` marks the explicit allowlist.
2. **Impersonation isolation**: When `impersonation_session_id` is present, the RBAC middleware MUST evaluate only the target user's roles (obtained from impersonation service). The impersonator superadmin role is never available during the impersonated request. Annotations in audit logs record both `actor_id=target_user` and `impersonator_id=superadmin`.
3. **Step-up gating on RBAC management**: All mutations on `/admin/roles/*` and `/admin/policies/reload` require `X-2FA-Verified: 1` (≤5 min) or `X-Re-Verify-Password` header with the current admin's password.
4. **Tier gating on role assignment**:
   - `admin` (tier 3) cannot assign `superadmin` (tier 4) nor promote a custom role to tier ≥3 when caller is tier 3;
   - Caller can only grant roles with strictly lower tier than themselves unless caller is superadmin.
5. **Deny overrides**: Casbin model uses `e = some(where (p.eft == allow)) && !some(where (p.eft == deny))`; explicit `deny` policy for a role/permission combo denies even if inheritance would otherwise grant it. Use for quarantine roles (e.g., `quarantined_editor` denies `post:publish` despite inheriting from `editor`).
6. **Superadmin bootstrap safety**: Superadmin is seeded ONLY via `app seed --bootstrap-superadmin` with env var `BOOTSTRAP_SUPERADMIN_EMAIL` + `BOOTSTRAP_SUPERADMIN_PASSWORD_HASH` (never plain). There MUST be exactly one or zero superadmin rows after seed; never create superadmin through the API.
7. **CSRF on state-changing RBAC endpoints**: Browser clients must send the `X-Csrf-Token` header on all non-GET `/admin/roles`, `/admin/policies`, `/admin/users/:id/roles` requests.
8. **Rate limits for RBAC**:
   - `POST /admin/policies/reload`: 60 req/sec per admin (throttled because it invalidates global cache);
   - `POST /admin/roles/:id/permissions`: 30 req/sec per admin;
   - `POST /admin/users/:id/roles`: 30 req/sec per admin;
   - Audit read endpoints follow admin analytics-tier limits.
9. **Model file immutability**: `internal/security/casbin/model.conf` (or equivalent embedded-FS file) is read-only at runtime. Admins cannot upload or modify the model file through APIs. Restarts + ops change control required for model changes.
10. **Policy write ordering**: Every write to Casbin policy → mirror write to `roles`/`user_roles`/`role_permissions` → audit log row → outbox event → Redis watcher `PUBLISH casbin.policy.updated` with HMAC signature of tuple set; all in one DB transaction except the PUBLISH which goes through the Watermill outbox.

## User Profile + Authentication Integration

- The existing `users` table carries no direct role column; roles flow through `user_role_assignments` mirror table + Casbin `g(user, role)` tuples.
- Login flow: after password + 2FA succeed, AuthService calls `RBACService.ResolveRolesAndPermissions(user_id)` which returns the flattened permission set via Casbin `GetImplicitPermissionsForUser`; JWT access token carries the role names only (not every permission) as a `roles` claim; permissions are re-resolved server-side from the enforcer on every request (to pick up live policy changes without forcing re-login).
- The token's `roles` claim is used purely for debugging/logging and quick UI badges; **never trust client-supplied roles claim** for decisions; always go through the enforcer.

## Management Interfaces (Admin UI)

### Pages

- **Admin → Roles → List** (`/admin/roles` in UI, data via `GET /api/v1/admin/roles`): table with name, tier, inherited_from, user_count, is_system, updated_at; actions: Create role, Edit, Delete (custom only).
- **Admin → Roles → Detail** (`/admin/roles/:id`): three tabs (Members, Permissions, Audit). Members shows assigned users with avatar, tier warning badge if granting cross-tier. Permissions tab shows a checkbox matrix grouped by resource category (RBAC, User, Post, Media, Settings, Analytics, Impersonation, Profile) with save requiring 2FA re-proof on changes to top-tier categories.
- **Admin → Permissions → Catalog** (`/admin/permissions`): registry list, read-only by default (only superadmin can toggle hidden/visible). Each permission shows description, resource, action, scope, default-role assignments.
- **Admin → Audit → Access** (`/admin/audit/access`): Enforcement events viewer with filters (decision, user_id, permission, path, method, date range, tier, impersonation=yes/no), CSV export button, and SSE live tail.

### Dynamic updates without restart

All mutations (`role create/edit/delete`, permission assignment add/remove, user ↔ role add/remove) complete the following pipeline atomically:

1. Validate tier gating;
2. Call Casbin adapter `AddPolicy` / `RemovePolicy` / `AddGroupingPolicy` / `RemoveGroupingPolicy` within the DB transaction;
3. Update mirror rows in `rbac_roles` / `rbac_permissions` / `user_role_assignments` / `role_permissions` inside the same transaction;
4. Insert `rbac_policy_audit_log` row with before/after diff;
5. Insert outbox event `rbac.policy.updated` with payload containing changed tuple set (HMAC signed);
6. Commit;
7. Outbox dispatcher publishes to Redis channel `casbin.policy.updated:{cluster}`; each enforcer process subscribes, coalesces for 200 ms with `sync.Map` debouncer, then calls `e.LoadPolicy()` once.

## Password Management and RBAC

Password management endpoints (`/me/password`, `/auth/password/*`) are RBAC-gated via self-service permissions `me.password:change` (all authenticated users) plus admin override `user.password.admin_reset`. Admin reset additionally requires `X-2FA-Verified` header.

## Privacy Settings Integration

Privacy setting endpoints (`GET/PUT /me/privacy`, cross-user `GET /admin/users/:id/profile`) use the standard permissions (`me.privacy:read:own`, `user.profile:edit:all`) through the same Casbin pipeline; no custom bypasses.

## Activity Tracking Integration

Activity tracking (`user_activity` table) records RBAC-policy-change actions as a new activity type enum: `rbac_role_created`, `rbac_role_updated`, `rbac_role_deleted`, `rbac_permission_assigned`, `rbac_permission_revoked`, `rbac_user_role_assigned`, `rbac_user_role_revoked`, `rbac_policy_reloaded` — surfaced on both `/me/activity` (owner only, own changes) and `/admin/users/:id/activity` (admin view).

## Responsive UI Design

- Breakpoints: Mobile (<768 px) stacks role detail tabs vertically with a horizontal scroll chips bar for resource categories; Desktop (≥1024 px) uses a left category sidebar, right matrix grid, top role summary card with tier badge.
- Tier badge color coding: tier 4 → purple pill "Superadmin", tier 3 → red "Admin", tier 2 → amber "Editor", tier 1 → green "Viewer"; custom roles carry a blue pill with tier number inside.
- Permission matrix: Mobile uses accordion per resource with a single vertical allow/deny switch per row; desktop shows checkboxes grouped by role in columns. Checkboxes for tier≥3 roles are disabled for admin-tier callers (tier gating).

## Testing Strategy

### Unit Tests

1. **Casbin model parse**: `model.conf` loads; `AddMatchFunc` for custom `keyMatch3` wildcard matching + scope matcher `scopeMatch(own,all)` works.
2. **Role inheritance transitivity**: `viewer < editor < admin < superadmin`; giving `post:edit` to editor implicitly grants it to admin/superadmin.
3. **Explicit deny overrides**: assign `quarantined_editor :> editor`, add deny `quarantined_editor, post:*`; enforce returns false even though editor base would allow.
4. **Tier gating logic**: `admin (3).CanAssignRole(tier=4) → false`; `superadmin.CanAssign(tier=4) → true`; custom role at tier 2 created by admin (tier 3) → allowed, created by editor (tier 2) → 403.
5. **Enforcer cache hit semantics**: same enforce tuple called twice returns <50 μs on second call (cache).
6. **Deny-fast short-circuit**: middleware returns 403 after ~100 μs without allocating controller.
7. **Redis watcher**: simulate publish from process A → process B sees policy changes within 500 ms in 99/100 trials; 200 ms p95 target.
8. **BatchEnforce**: 100-tuple batch in <1 ms with cached enforcer.

### Integration Tests

9. **Middleware × permission matrix**: Iterate every endpoint × role × expected decision (all 4 system roles); check HTTP status == expected; ensure 0 cross-role false positives.
10. **Impersonation session enforcement**: Superadmin impersonates viewer → requesting `admin-only` route returns 403 (inherits target roles only).
11. **Cross-role escalation attempt**: Editor calls `POST /admin/users/:id/roles` attempting to assign `admin` → 403 `rbac.user.cannot_assign_higher_tier`; audit log records attempt.
12. **Policy hot-reload consistency**: Process A issues `POST /admin/roles/:id/permissions` to add a permission; process B issues `GET /protected-endpoint` that needs that permission within 300 ms → 200 (p95); fails only if Redis watcher unavailable (degraded mode falls back to periodic 10 s Poller).
13. **Audit log coverage**: Run 200 randomized requests mix, count rbac_enforcement_event rows = 200; every row has decision + user_id + permission + latency; failed decisions include `deny_reason_code`.
14. **DB-degraded mode**: Kill Redis → enforcer uses DB adapter direct (latency 2–3 ms) + logs a warning; no 5xx on enforce (still correct decisions).

### Edge Cases

15. User has 10+ roles (custom scenario): flattening returns union without duplicates; enforce is still O(1) via cache.
16. Remove a role that users still hold → user_role_assignments rows set to orphan state? No: API enforces "delete role first removes all assignments" (two-step confirmation UI); backend delete endpoint cascades assignments first, then role; all inside tx.
17. Update role tier to a value that exceeds caller tier → 403.
18. Simultaneous writes to same role's permissions → optimistic locking via `rbac_roles.version`; conflicts return 409 `rbac.role.version_conflict`.
19. `BatchEnforce` with empty result set → returns `[]bool` length 0 correctly, no panic.
20. Unauthenticated request (no JWT) → middleware 401 before RBAC ever consulted; no event written to rbac audit (auth audit captures it).

## Security Audit Checklist (RBAC-specific)

1. **Model file in embed.FS only**: `go:embed model.conf`; verify at deploy time `app doctor --rbac-model-hash` matches expected SHA-256 (stored in deployment manifest).
2. **Redis watcher message signing**: All `casbin.policy.updated` payloads carry HMAC-SHA256 of `payload || nonce || RBAC_WATCHER_HMAC_KEY`; subscriber rejects unsigned messages. Prevents rogue publishes from compromised app nodes.
3. **Superadmin bootstrap key rotation**: BOOTSTRAP_SUPERADMIN env vars one-shot; `app seed --bootstrap-superadmin --once` creates the user only if zero superadmins exist; never runs again. Running it a second time is a no-op + audit warning.
4. **Audit log integrity**: `rbac_policy_audit_log` table append-only via triggers (Postgres RULE or trigger rejects UPDATE/DELETE except for superadmin-curator role).
5. **Enforcer hot path never hits DB in steady state**: Cache hit rate ≥99.9%; integration test verifies DB query count ≤1 per 1000 enforcement calls when Redis watcher is healthy.
6. **No JWT permissions claim trust**: Pen-test check: craft JWT with `roles: ["superadmin"]` but DB has viewer → enforcer still returns 403 because it reads DB/Casbin policy, not JWT roles.
7. **CSRF token checked before RBAC writes**: POST/PUT/DELETE `/admin/roles*` and `/admin/users/*/roles` must have correct CSRF cookie+header pair even with valid JWT.
8. **Deny decisions throttled**: Repeated 403s from same IP + user combo → hit 429 throttle tier `rbac.deny_storm` (10/min → 429 Retry-After).
9. **Custom roles cannot create wildcard `*:*` permission**: Registry blocks; only superadmin system role carries that policy (seeded, non-editable).
10. **Mirror table consistency**: Nightly `app doctor --rbac-mirror-consistency` compares `casbin_rules` p/g tuples against `user_role_assignments` + `role_permissions`. Any mismatch → critical alert + auto-repair option (write Casbin → mirror, always Casbin wins).
11. **Scope matcher tests**: `scopeMatch(own, all)` matches only when policy scope is all or object scope == own; custom scope strings (like post UUIDs) validated against regex.
12. **Deleted user cleanup**: Hard delete user → ON DELETE CASCADE removes `g(user,*)` rows from `casbin_rules`; FK mirror table also cascade deletes.
13. **Policy reload endpoint not DDoS-able**: Require `rbac.policy:reload` permission + rate limit + 2FA; endpoint is never exposed publicly (only admin subdomain).

## Staging Deploy & UAT

### Deployment steps

1. Build container image tagged `rbac-sprint-e`.
2. Run migrations: `app migrate up` — creates `casbin_rules`, `rbac_roles`, `rbac_permissions`, `user_role_assignments`, `rbac_policy_audit_log`, `rbac_enforcement_events` tables; seeds system roles + registry permissions; runs `app seed --rbac-only` on first staging deploy.
3. Smoke check: `app doctor --rbac-smoke` verifies model loads, seed roles exist, admin→editor→viewer inheritance passes, enforcer latency < 1 ms cached, Redis watcher subscribes successfully.
4. Smoke HTTP tests: 403 for unauthenticated → 200 after login with correct role.
5. Admin UAT checklist:
   1. Create a custom role `senior_editor` inheriting from `editor` + assigned `post:delete:own`. Verify that `senior_editor` user can delete own posts but not others' posts (admin only).
   2. Attempt to assign superadmin role as admin → 403. Assign as superadmin → 200.
   3. Change a permission for `editor` → reload on replica pod within 500 ms (curl to second pod endpoint → 200 after change, previously 403).
   4. Export RBAC audit CSV → contains all enforcement events with correct decision codes.
   5. Revoke `post:publish` from `editor` → all editors instantly lose publish ability (p95 <200 ms to all pods).
   6. Access audit viewer filters → filters correctly; impersonated actions carry impersonator column.
   7. `POST /auth/password/reset` → 200 even for non-superadmin users (correct tier gating on forgot vs admin-reset).
   8. Privacy toggle `public→private` → requires self-service permission + step-up; succeeds for owners; fails for cross-user unless admin.
6. Rollback strategy: Kubernetes `kubectl rollout undo` + reversible down migrations (`app migrate down`) + feature flag `RBAC_USE_CASBIN=false` falls back to legacy direct role_permissions table checks (kept as dual-write during first production week).

## How to Add a New Role

1. Open Admin → Roles → Create role.
2. Set name (snake_case enforced), display name, tier (strictly less than your own tier unless superadmin), select "Inherits from" role (pick viewer/editor/admin only).
3. On the Permissions tab, select which resource:action entries apply; save with 2FA proof.
4. Assign users in the Members tab; assignment is active immediately across all replicas within ≤200 ms.
5. Add unit + integration tests for new role × critical endpoints in the testing spec; run `go test ./...`.

## How to Add a New Permission

1. Register a new entry in `internal/security/rbac/registry.go` (or equivalent file per codebase): key `resource:action[:scope]`, description, resource, action, scope, default-to-role list (empty = only explicit grants).
2. Run `app seed --rbac-only` in staging to write the registry entry to `rbac_permissions`.
3. Run `app doctor --rbac-registry` to confirm no orphaned permissions.
4. In OpenAPI, annotate the new endpoint with `x-rbac-permission: "resource:action:scope"`; middleware reads it from route metadata at boot.
5. Add matrix rows to this doc's Permission table.

## Cross-References

- [authentication.md](file:///mnt/myadrive/repo/turahe/blog-api/docs/features/authentication.md) — login, JWT claims, step-up, impersonation
- [user-profile-management.md](file:///mnt/myadrive/repo/turahe/blog-api/docs/features/user-profile-management.md) — profile, password, privacy endpoints that consume the RBAC middleware
- [impersonation.md](file:///mnt/myadrive/repo/turahe/blog-api/docs/features/impersonation.md) — impersonation session gating rules
- [settings-management.md](file:///mnt/myadrive/repo/turahe/blog-api/docs/features/settings-management.md) — settings sensitivity tiers (admin_only, server_only) consumed by RBAC scope enforcement
- [user-management.md](file:///mnt/myadrive/repo/turahe/blog-api/docs/features/user-management.md) — existing roles/permissions entities referenced by mirror layer
- [rbac-casbin.md](file:///mnt/myadrive/repo/turahe/blog-api/docs/backend/rbac-casbin.md) — hexagonal module placement, ports, service, events

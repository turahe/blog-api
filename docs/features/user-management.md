# User Management

## Scope

- create users
- update users
- activate, deactivate, or suspend users
- assign and revoke roles
- manage permission sets through roles
- manage user avatar references to media_assets

## Rules

- only authorized admins can manage other users
- users can update their own allowed profile fields only
- role and permission changes must be audited
- permission checks must be enforced server-side

## Key Entities

- users
- roles
- permissions
- user_roles
- role_permissions
- media_assets (for user avatar_id reference)

## Key Permissions

- `user.read`
- `user.create`
- `user.update`
- `user.delete`
- `role.read`
- `role.manage`
- `analytics.read`
- `analytics.export`
- `analytics.search.read`
- `analytics.realtime.read`
- `settings.read`
- `settings.update`
- `settings.history.read`
- `settings.storage.read`
- `settings.storage.update`
- `settings.smtp.read`
- `settings.smtp.update`
- `settings.smtp.test_send`
- `impersonation.start`
- `impersonation.stop`
- `impersonation.audit.read`
- `impersonation.target_user.read`

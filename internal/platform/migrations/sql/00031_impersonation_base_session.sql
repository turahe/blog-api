-- +goose Up
-- Bind each impersonation session to the impersonator's own sign-in (refresh-session family):
-- once that sign-in is logged out, revoked, or expired, the impersonation token stops working.
-- Sessions opened before this column existed have no base and end on their next use.
ALTER TABLE impersonation_sessions ADD COLUMN base_family_id uuid;
ALTER TABLE impersonation_sessions DROP CONSTRAINT impersonation_sessions_end_reason_check;
ALTER TABLE impersonation_sessions ADD CONSTRAINT impersonation_sessions_end_reason_check
    CHECK (end_reason IN ('manual_exit', 'expired', 'policy', 'parent_session_expired'));

-- +goose Down
UPDATE impersonation_sessions SET end_reason = 'policy' WHERE end_reason = 'parent_session_expired';
ALTER TABLE impersonation_sessions DROP CONSTRAINT impersonation_sessions_end_reason_check;
ALTER TABLE impersonation_sessions ADD CONSTRAINT impersonation_sessions_end_reason_check
    CHECK (end_reason IN ('manual_exit', 'expired', 'policy'));
ALTER TABLE impersonation_sessions DROP COLUMN IF EXISTS base_family_id;

-- +goose Up
-- Editable copy for notifications, one row per (type, channel). Rows are seeded
-- from the built-in catalogue by `app seed`; a missing row falls back to it.
CREATE TABLE notification_templates (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    type text NOT NULL CHECK (char_length(type) BETWEEN 1 AND 100),
    channel text NOT NULL CHECK (channel IN ('email', 'web', 'sse')),
    subject text NOT NULL DEFAULT '' CHECK (char_length(subject) <= 200),
    title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 200),
    body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 20000),
    preview text NOT NULL DEFAULT '' CHECK (char_length(preview) <= 500),
    event text NOT NULL DEFAULT '' CHECK (char_length(event) <= 100),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX notification_templates_type_channel_unique
    ON notification_templates (type, channel);

-- +goose Down
DROP TABLE IF EXISTS notification_templates;

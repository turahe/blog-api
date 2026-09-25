-- +goose Up
-- One row per message a worker consumer has handled, so a redelivered message is skipped.
-- The row is written in the handler's transaction and pruned after the dedupe window.
CREATE TABLE processed_messages (
    consumer     text        NOT NULL,
    message_id   text        NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, message_id)
);

CREATE INDEX processed_messages_processed_at_idx ON processed_messages (processed_at);

-- +goose Down
DROP TABLE IF EXISTS processed_messages;

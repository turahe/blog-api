-- +goose Up
-- Public sign-ups waiting for email verification. The users row is created only when the
-- emailed token is used together with the sign-up password, so an unverified sign-up reserves
-- neither the address nor the username, and whoever controls the inbox cannot activate an
-- account someone else chose the password for. Rows are deleted on use or once expired.
CREATE TABLE registrations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    email text NOT NULL,
    username text NOT NULL,
    full_name text NOT NULL,
    password_hash text NOT NULL,
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX registrations_email_idx ON registrations (lower(email));
CREATE INDEX registrations_expires_idx ON registrations (expires_at);

-- +goose Down
DROP TABLE IF EXISTS registrations;

-- +goose Up
ALTER TABLE users
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE users
    ADD CONSTRAINT users_email_key UNIQUE (email);

-- +goose Down
ALTER TABLE users DROP CONSTRAINT users_email_key;

ALTER TABLE users
    DROP COLUMN updated_at,
    DROP COLUMN created_at;

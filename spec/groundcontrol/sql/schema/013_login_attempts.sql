-- +goose Up
CREATE TABLE login_attempts (
  id SERIAL PRIMARY KEY,
  username VARCHAR(255) UNIQUE NOT NULL,
  failed_count INT NOT NULL DEFAULT 0,
  locked_until TIMESTAMP,
  last_attempt TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE login_attempts;

-- +goose Up

CREATE TABLE robot_accounts (
  id SERIAL PRIMARY KEY,
  robot_name VARCHAR(255) UNIQUE NOT NULL,
  robot_secret_hash VARCHAR(255) NOT NULL,
  robot_id VARCHAR(255) UNIQUE NOT NULL,
  satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
  robot_expiry TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE robot_accounts;

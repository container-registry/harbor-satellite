-- +goose Up

CREATE TABLE robot_accounts (
  id SERIAL PRIMARY KEY,
  robot_name VARCHAR(255) UNIQUE NOT NULL,
  robot_secret VARCHAR(255) NOT NULL,
  satellite_id INT NOT NULL REFERENCES satellites(id) ON DELETE CASCADE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE robot_accounts;

-- +goose Up

CREATE TABLE configs (
  id SERIAL PRIMARY KEY,
  config_name VARCHAR(255) UNIQUE NOT NULL,
  registry_url VARCHAR(255) NOT NULL,
  config JSONB NOT NULL,
  created_at TIMESTAMP DEFAULT NOW() NOT NULL,
  updated_at TIMESTAMP DEFAULT NOW() NOT NULL
);

-- +goose Down
DROP TABLE configs;

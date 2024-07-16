-- +goose Up

CREATE TABLE groups (
  id SERIAL PRIMARY KEY,
  group_name VARCHAR(255) UNIQUE NOT NULL,
  username VARCHAR(255) NOT NULL,
  password VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW() NOT NULL,
  updated_at TIMESTAMP DEFAULT NOW() NOT NULL
);

-- +goose Down
DROP TABLE groups;


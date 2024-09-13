
-- +goose Up

CREATE TABLE projects (
  id SERIAL PRIMARY KEY,
  projects VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT NOW() NOT NULL,
  updated_at TIMESTAMP DEFAULT NOW() NOT NULL
);

-- +goose Down
DROP TABLE projects;


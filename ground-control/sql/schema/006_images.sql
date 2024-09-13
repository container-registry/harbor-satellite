-- +goose Up

CREATE TABLE images (
  id SERIAL PRIMARY KEY,
  registry VARCHAR(255) NOT NULL,
  repository VARCHAR(255) NOT NULL,
  tag VARCHAR(255) NOT NULL,
  digest VARCHAR(255) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE images;

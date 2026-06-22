-- +goose Up

CREATE TABLE config_digests (
    config_id INT PRIMARY KEY REFERENCES configs(id) ON DELETE CASCADE,
    digest VARCHAR(255) NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE satellite_desired_states (
    satellite_id INT PRIMARY KEY REFERENCES satellites(id) ON DELETE CASCADE,
    expected_state_digest VARCHAR(255),
    expected_config_digest VARCHAR(255),
    last_converged_at TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE satellite_desired_states;
DROP TABLE config_digests;

# Installation Guide

This guide provides detailed instructions for installing and setting up Harbor Satellite in your environment. Follow these steps carefully to ensure a successful installation.

## System Requirements

### Hardware Requirements
- CPU: 2+ cores
- Memory: 4GB+ RAM
- Storage: 20GB+ available space
- Network: Stable internet connection for initial setup

### Software Requirements
- Linux/Unix-based operating system
- Docker 20.10 or later
- Dagger 0.9 or later
- Go 1.21 or later (for development)
- PostgreSQL 14 or later (for Ground Control)

## Installation Steps

### 1. Install Dependencies

First, install the required dependencies:

```bash
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Install Dagger
curl -L https://dl.dagger.io/dagger/install.sh | sh

# Install Go (for development)
wget https://golang.org/dl/go1.21.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.21.0.linux-amd64.tar.gz
```

### 2. Set Up Ground Control

#### 2.1. Configure Environment Variables

Create a `.env` file in the `ground-control` directory:

```bash
# Harbor Registry Configuration
HARBOR_USERNAME=admin
HARBOR_PASSWORD=your_password
HARBOR_URL=your_harbor_url

# Application Configuration
PORT=8080
APP_ENV=local

# Database Configuration
DB_HOST=localhost
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres
DB_PASSWORD=your_db_password
```

#### 2.2. Set Up Database

```bash
# Install PostgreSQL
sudo apt-get update
sudo apt-get install postgresql postgresql-contrib

# Create database and user
sudo -u postgres psql
CREATE DATABASE groundcontrol;
CREATE USER groundcontrol WITH PASSWORD 'your_db_password';
GRANT ALL PRIVILEGES ON DATABASE groundcontrol TO groundcontrol;
\q

# Run database migrations
cd sql/schema
goose postgres "postgres://groundcontrol:your_db_password@localhost:5432/groundcontrol?sslmode=disable" up
```

#### 2.3. Start Ground Control

```bash
# Using Dagger
dagger call run-ground-control up

# Or build and run directly
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
./gc-dev
```

### 3. Set Up Satellite

#### 3.1. Configure Satellite

Create a `config.json` file in the root directory:

```json
{
  "environment_variables": {
    "ground_control_url": "http://localhost:8080",
    "log_level": "info",
    "use_unsecure": true,
    "zot_config_path": "./registry/config.json",
    "token": "your_satellite_token",
    "jobs": [
      {
        "name": "replicate_state",
        "schedule": "@every 00h00m10s"
      },
      {
        "name": "update_config",
        "schedule": "@every 00h00m30s"
      },
      {
        "name": "register_satellite",
        "schedule": "@every 00h00m05s"
      }
    ],
    "local_registry": {
      "url": "",
      "username": "",
      "password": "",
      "bring_own_registry": false
    }
  }
}
```

#### 3.2. Build and Run Satellite

```bash
# Build satellite binary
dagger call build --source=. --component=satellite export --path=./bin

# Run satellite
./bin/satellite
```

### 4. Register Satellite with Ground Control

```bash
# Create a group
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "group": "my-group",
  "registry": "your_registry_url",
  "artifacts": [
    {
      "repository": "your_project/your_image",
      "tag": ["latest"],
      "type": "docker",
      "digest": "your_image_digest",
      "deleted": false
    }
  ]
}'

# Register satellite
curl --location 'http://localhost:8080/satellites/register' \
--header 'Content-Type: application/json' \
--data '{
    "name": "my-satellite",
    "groups": ["my-group"]
}'
```

## Verification

### 1. Check Ground Control Health

```bash
curl --location 'http://localhost:8080/health'
```

### 2. Verify Satellite Connection

Check the satellite logs for successful registration and connection:

```bash
tail -f satellite.log
```

### 3. Test Image Pull

```bash
# Configure Docker to use local registry
docker pull localhost:5000/your_project/your_image:latest
```

## Troubleshooting

### Common Issues

1. **Database Connection Issues**
   - Verify PostgreSQL is running
   - Check database credentials
   - Ensure database exists

2. **Satellite Registration Failures**
   - Verify Ground Control is running
   - Check network connectivity
   - Validate token

3. **Image Pull Failures**
   - Check registry configuration
   - Verify image exists
   - Check network connectivity

### Getting Help

- Join the [CNCF Slack channel](https://cloud-native.slack.com/archives/C06NE6EJBU1)
- Review [GitHub Issues](https://github.com/goharbor/harbor-satellite/issues)

## Next Steps

1. [Configuration Guide](../user-guide/configuration.md) - Configure your deployment
2. [User Guide](../user-guide/README.md) - Learn how to use Harbor Satellite

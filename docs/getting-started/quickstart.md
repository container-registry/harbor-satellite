## Quickstart: Local Setup of Harbor Satellite from Source

This guide helps you set up Harbor Satellite and Ground Control locally, both with and without Dagger. It includes support for both manual group creation and full satellite configuration.

### 1. Prerequisites

Ensure you have:

* A Harbor registry instance (or any OCI-compliant registry).
* Credentials to create robot accounts in the registry.
* [Dagger](https://dagger.io) (latest version).
* [Goose](https://github.com/pressly/goose) for DB migrations.

### 2. Set Up the Registry

We'll assume you use a Harbor instance.

- **Registry Login**: Obtain the username and password for your registry, ensuring it has appropriate permissions.

### 3. Configure Ground Control
Navigate to `ground-control/` and configure your environment.

```bash
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://demo.goharbor.io
```

**With Dagger:**

```bash
PORT=8080
APP_ENV=local
DB_HOST=pgservice
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       # Customize based on your DB config
DB_PASSWORD=password       # Customize based on your DB config
```

**Without Dagger:**

```bash
PORT=8080
APP_ENV=local
DB_HOST=127.0.0.1
DB_PORT=8100
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       # Customize based on your DB config and add the same config to the docker-compose file
DB_PASSWORD=password       # Customize based on your DB config and add the same config to the docker-compose file
```

Make sure to match these DB credentials in `docker-compose.yml`.

### 4. Start Ground Control
To start the Ground Control service, There are two ways:

**With Dagger:**

```bash
dagger call run-ground-control up
```

Build binary:

```bash
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
./gc-dev
```

**Without Dagger:**

To start the Ground Control service without using Dagger, follow these steps:
First, move to the ground-control directory

```bash
cd ..
```

Then add the credentials to the `docker-compose` file for the Postgres service and pgAdmin, and start the services using. Make sure you add the same credentials that you have added in the .env file

```bash
docker compose up
```
### 5. Configure migrations.
Once the services are up, move to the `sql/schema` folder to set up the database required for the ground control

```bash
cd sql/schema
```

Install `goose` to run database migrations if not already installed

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

Now run the below command with the credentials that you have added in the docker-compose file

```bash
goose postgres "postgres://<POSTGRES_USER>:<POSTGRES_PASSWORD>@localhost:8100/groundcontrol?sslmode=disable" up
```

Now you can start the `ground-control` using the below command by first moving to the directory and running

```bash
cd ../..
go run main.go
```

### 6. Health Check

```bash
curl --location 'http://localhost:8080/health'
```

---

### 7. Create Group (Optional)

A group is a set of artifacts the satellite will monitor. This is usually created by the Harbor adapter, but for testing you can use:

- Now we create a group. To create a group, use the following `curl` command
> **Note:** Please modify the body given below according to your registry
``` bash
curl --location 'http://localhost:8080/groups/sync' \
--   'Content-Type: application/json' \
--data '{
  "group": "GROUP_NAME",
  "registry": "YOUR_REGISTRY_URL",
  "artifacts": [
    {
      "repository": "YOUR_PROJECT/YOUR_IMAGE",
      "tag": ["TAGS OF THE IMAGE"],
      "type": TYPE_OF_IMAGE,
      "digest": "DIGEST",
      "deleted": false
    }
  ]
}
'
```
Example Curl Command for creating `GROUP`
```bash
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
--data '{
  "group": "group1",
  "registry": "https://demo.goharbor.io",
  "artifacts": [
    {
      "repository": "alpine/alpine",
      "tag": ["latest"],
      "type": "docker",
      "digest": "sha256:3e21c52835bab96cbecb471e3c3eb0e8a012b91ba2f0b934bd0b5394cd570b9f",
      "deleted": false
    }
  ]
}'
```

You may also skip this if you are using pre-defined upstream groups.

---

### 8. Create Config Artifact

Required if you're using config-based registration:

```bash
curl --location 'http://localhost:8080/configs/sync' \
--header 'Content-Type: application/json' \
--data '{
  "config_name": "config1",
  "registry": "http://localhost:5454",
  "config": {
    "state_config": {},
    "app_config": {
      "ground_control_url": "http://127.0.0.1:8080",
      "log_level": "info",
      "use_unsecure": true,
      "zot_config_path": "./registry_config.json",
      "state_replication_interval": "@every 10s",
      "update_config_interval": "@every 10s",
      "register_satellite_interval": "@every 10s",
      "local_registry": {
        "url": "http://127.0.0.1:8585"
      }
    },
    "zot_config": {
      "distSpecVersion": "1.1.0",
      "storage": {
        "rootDirectory": "./zot"
      },
      "http": {
        "address": "127.0.0.1",
        "port": "8585"
      },
      "log": {
        "level": "info"
      }
    }
  }
}'
```

### 9. Register Satellite with Ground Control.
Register the satellite and get the token(eg: 6d6b0ad49cfbf63550b29f7220bd78f3).

```bash
curl --location 'http://localhost:8080/satellites/register' \
--header 'Content-Type: application/json' \
--data '{
    "name": "satellite_1",
    "groups": ["group1"],
    "config_name": "config1"
}'
```
> **Note**: Running the above command would produce a token which is important for the satellite to register itself to the ground control
- Once you have the token for the satellite, we can move on to the satellite to configure it.
---

### 10. Configure and Run Satellite
Return to the root project directory:
In the `config.json` file, add the following configuration

```json
{
  "environment_variables": {
    "ground_control_url": "http://127.0.0.1:8080", // URL for the ground control server
    "log_level": "info", // Log level: can be "debug", "info", "warn", or "error"
    "use_unsecure": true, // Use unsecure connections (set to true for dev environments)
    "zot_config_path": "./registry/config.json", // Path to Zot registry configuration file
    "token":"ADD_THE_TOKEN_FROM_THE_ABOVE_STEP", // add the token received while registering satellite from the below step
    "jobs": [
      // List of scheduled jobs
      // Checkout https://pkg.go.dev/github.com/robfig/cron#hdr-Predefined_schedules for more
      //  details on how to write the cron job config
      {
        "name": "replicate_state", // Job to replicate state
        "schedule": "@every 00h00m10s" // Schedule interval: every 10 seconds
      },
      {
        "name": "update_config", // Job to update configuration
        "schedule": "@every 00h00m30s" // Schedule interval: every 30 seconds
      },
      {
        "name": "register_satellite", // Job to register satellite
        "schedule": "@every 00h00m05s" // Schedule interval: every 5 seconds
      }
    ],
    "local_registry": {
      // Configuration for the local registry
      "url": "", // Add your own registry URL if bring_own_registry is true else leave blank
      "username": "", // Add your own registry username if bring_own_registry is true else leave blank
      "password": "", // Add your own registry password if bring_own_registry is true else leave blank
      "bring_own_registry": false // Set to true if using an external registry and the above config
    }
  }
}
```

- Add the token to `.env` file.

Run the satellite:

```bash
go run cmd/main.go
```

Or build binary:

> **Note**: You can also build the satellite binaries and use them.
- To build the binary of the satellite, use the following command

```bash
dagger call build --source=. --component=satellite export --path=./bin
./bin/<your-arch>
```
- This would generate the binaries for various architectures in the `bin` folder. Choose the binary for your system and use it. Make sure that the `config.json` and the binary directory are the same when running it otherwise it would throw an error.

**Directly passing env in command**

```bash
go run cmd/main.go --token "<token>" --ground-control-url "<ground-control-url>"
```

## Quickstart: Local Setup of Harbor Satellite from Source

To set up Harbor Satellite locally, follow these steps:

### 1. Prerequisites
Ensure you have:
- A Harbor registry instance (or similar OCI-compliant registry).
- Credentials with permission to create robot accounts in the registry.
- The latest version of Dagger installed.

### 2. Set Up the Registry
In this guide, we'll use a Harbor registry instance.

- **Registry Login**: Obtain the username and password for your registry, ensuring it has appropriate permissions.

### 3. Configure Ground Control
Navigate to the `ground-control` directory and set up the following environment variables:

- For running ground control using Dagger

```bash
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://demo.goharbor.io

PORT=8080
APP_ENV=local

DB_HOST=pgservice
DB_PORT=5432
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       # Customize based on your DB config
DB_PASSWORD=password       # Customize based on your DB config
```

- For running ground control without Dagger

```bash
HARBOR_USERNAME=admin
HARBOR_PASSWORD=Harbor12345
HARBOR_URL=https://demo.goharbor.io

PORT=8080
APP_ENV=local

DB_HOST=127.0.0.1
DB_PORT=8100
DB_DATABASE=groundcontrol
DB_USERNAME=postgres       # Customize based on your DB config and add the same config to the docker-compose file
DB_PASSWORD=password       # Customize based on your DB config and add the same config to the docker-compose file
```

### 4. Run Ground Control
To start the Ground Control service, execute the following Dagger command:

```bash
dagger call run-ground-control up
```

You can also build ground-control binary using the below command

```bash
dagger call build-dev --platform "linux/amd64" --component "ground-control" export --path=./gc-dev
```

To Run ground-control binary use

```bash
./gc-dev
```

> **Note:** Ensure you have set up Dagger with the latest version before running this command. Ground Control will run on port 8080.

#### Without Using Dagger

To start the Ground Control service without using Dagger, follow these steps:
First, move to the ground-control directory

```bash
cd ..
```

Then add the credentials to the `docker-compose` file for the Postgres service and pgAdmin, and start the services using. Make sure you add the same credentials that you have added in the .env file

```bash
docker compose up
```

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

### 6. Register the Satellite with Ground Control

Once the ground control is up and running, you can check its health status using the following curl command

To test the health of the server, use the following `curl` command:

```bash
curl --location 'http://localhost:8080/health'
```

- Now we create a group. To create a group, use the following `curl` command
> **Note:** Please modify the body given below according to your registry
``` bash
curl --location 'http://localhost:8080/groups/sync' \
--header 'Content-Type: application/json' \
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
}
'
```
- Once the group is created, now we would add a satellite to the group so that the satellite would be available to track the images/artifacts present in the group

Below curl command is used to register a satellite which also provides the authentication token for the satellite
```bash
curl --location 'http://localhost:8080/satellites' \
--header 'Content-Type: application/json' \
--data '{
    "name": "SATELLITE_NAME",
    "groups": ["GROUP_NAME"]
}'
```
> **Note**: Running the above command would produce a token which is important for the satellite to register itself to the ground control
- Once you have the token for the satellite, we can move on to the satellite to configure it.
### 6. Configure Satellite

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
- Now start the satellite using the following command from the root directory.
```bash
go run cmd/main.go
```
> **Note**: You can also build the satellite binaries and use them.
- To build the binary of the satellite, use the following command
```bash
dagger call build --source=. --component=satellite export --path=./bin
```
- This would generate the binaries for various architectures in the `bin` folder. Choose the binary for your system and use it. Make sure that the `config.json` and the binary directory are the same when running it otherwise it would throw an error.

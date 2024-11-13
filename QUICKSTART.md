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

### 5. Configure Satellite
Return to the root project directory:

```bash
cd ..
```

Then navigate to the `satellite` directory and verify that `config.toml` is set up correctly:

```toml
# Whether to use the built-in Zot registry or not
bring_own_registry = false

# IP address and port of the registry
own_registry_adr = "127.0.0.1"
own_registry_port = "8585"

# URL of remote registry or local file path
url_or_file = "https://demo.goharbor.io/v2/myproject/album-server"



# Default path for Zot registry config.json
zotConfigPath = "./registry/config.json"

# Set logging level
log_level = "info"
```

### 6. Register the Satellite with Ground Control
Using `curl` or Postman, make a `POST` request to register the Satellite with Ground Control:

```bash
curl -X POST http://localhost:8080/satellites/register -H "Content-Type: application/json" -d '{ "name": "<satellite_name_here>" }'
```

The response will include a token string. Set this token in the Satellite `.env` file:

```console
TOKEN=<string_from_ground_control>
```

### 7. Build the Satellite
Run the following Dagger command to build the Satellite:

```bash
dagger call build-dev --platform "linux/amd64" --component satellite export --path=./satellite-dev
```

To Run Satellite:
```bash
./satellite-dev
```

The Satellite service will start on port 9090. Ensure that the `ground_control_url` is correctly set in the Satellite configuration before launching.


### 8. Finalize and Run
After setting the token, you can now run the Satellite. This setup will launch the Satellite in a container with the following exposed ports:
- **9090** for the Satellite service.
- **8585** for the Zot registry (if configured).

With this setup, your Harbor Satellite should be up and running!

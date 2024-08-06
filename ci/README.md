# Harbor Satellite CI/CD Pipeline

This repository uses [Dagger](https://docs.dagger.io/) for testing, building, and releasing Harbor Satellite.

## Setting Up Dagger for Harbor Satellite

Follow the steps below to set up Dagger:

### 1. Install Dagger CLI

Choose your operating system and install the Dagger CLI:

- **macOS:**
  ```sh
  brew install dagger/tap/dagger
- **Windows:**
    ```sh
    Invoke-WebRequest -UseBasicParsing -Uri https://dl.dagger.io/dagger/install.ps1 | Invoke-Expression; Install-Dagger
- **Linux:**
    ```sh
    curl -L https://dl.dagger.io/dagger/install.sh | BIN_DIR=$HOME/.local/bin sh

### 2. Verify Installation
Run the following command to verify the Dagger installation:
 - ```sh
    dagger --version
If you encounter any errors, refer to the [detailed installation guide](https://docs.dagger.io/install).

### 3. Generate Go Code
Once the Dagger CLI is installed, navigate to the root folder of harbor-satellite and run:

- ```sh
    dagger develop

This command will generate Go code in the ./ci folder.

### 4. Folder Structure
After running dagger develop, your ./ci folder should contain the following structure:
- ```sh
    ci/
    ├── dagger.gen.go
    ├── ground_control.go
    ├── internal
    │   ├── dagger
    │   ├── querybuilder
    │   └── telemetry
    ├── satellite.go
    ├── README.md
    ├── release.sh
    └── utils.go
If you encounter any issues during this process, consult the [Dagger Quickstart Guide](https://docs.dagger.io/quickstart/daggerize) for more detailed instructions.

## Running Dagger Functions
To view available functions, run:
- ```sh
    dagger functions
To run a particular function, run:
- ```sh
    dagger call <function_name> --args
- #### Example: Building Satellite Binaries
    To build the satellite binaries, use the following command:
    - ```sh
        dagger call build --source=. --name=satellite
This would spin up a container and install required dependencies and build various architecture binaries

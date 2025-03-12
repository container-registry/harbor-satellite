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
        dagger call build --source=. --name=satellite export --path=./bin
    This would spin up a container and install required dependencies and build various architecture binaries and export them to the host on path ./bin for testing on the host.
- #### Example: Releasing to GitHub
    To release the project on GitHub, use the following command
    - ```sh
        dagger call release --directory=. --token=<your_github_token>  --name=satellite
    The above function would then proceed to release the project on github for the name provided. The above function also takes argument `--release-type` which would tell the release what kind of release it is i.e major, minor or path, The default value is set to be path release
- #### Example: Releasing to GitHub with type of release
    To release the project on GitHub, use the following command
    - ```sh
        dagger call release --directory=. --token=<your_github_token>  --name=satellite --release-type=minor
The above function would release the minor version for the project mentioned
- #### Example: Running test cases using dagger
    To run the test cases using dagger use the following command
    - ```sh
        dagger run go test ./... -v -count=1
    This would run the test cases present in the entire project.
    To run the test cases without dagger use the following command
    - ```sh
        go test ./... -v -count=1 -args -abs=false
    This would set the config file to use the relative path.

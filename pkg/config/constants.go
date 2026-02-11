package config

// Job names that the user is expected to provide in the config.json file
const ReplicateStateJobName string = "replicate_state"
const ZTRConfigJobName string = "register_satellite"
const StatusReportJobName string = "status_report"
const SPIFFEZTRConfigJobName string = "spiffe_register_satellite"

// Default SPIFFE endpoint socket
const DefaultSPIFFEEndpointSocket string = "unix:///run/spire/sockets/agent.sock"

// The values below contain the default values of the constants used in the satellite. The user is allowed to override them
// by providing values in the config.json file. These default values will be used if the user does not provide any value or wrong format value
// in the config.json file.

// Registry data directory environment variable
const RegistryDataDirEnvVar string = "REGISTRY_DATA_DIR"

// Below are the default values of the job schedules that would be used if the user does not provide any schedule or
// if there is any error while parsing the cron expression
const DefaultZTRCronExpr string = "@every 00h00m05s"
const DefaultFetchAndReplicateCronExpr string = "@every 00h00m30s"
const DefaultHeartbeatCronExpr string = "@every 00h00m30s"

const BringOwnRegistry bool = false

const DefaultZotConfigJSON = `{
  "distSpecVersion": "1.1.0",
  "storage": {
    "rootDirectory": "./zot"
  },
  "http": {
    "address": "0.0.0.0",
    "port": "8585"
  },
  "log": {
    "level": "info"
  }
}`

const DefaultRemoteRegistryURL = "http://127.0.0.1:8585"
const DefaultGroundControlURL = "http://127.0.0.1:8080"


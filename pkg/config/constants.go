package config

// Job names that the user is expected to provide in the config.json file
const ReplicateStateJobName string = "replicate_state"
const ZTRConfigJobName string = "register_satellite"

// The values below contain the default values of the constants used in the satellite. The user is allowed to override them
// by providing values in the config.json file. These default values will be used if the user does not provide any value or wrong format value
// in the config.json file.

// Default config.json path for the satellite, used if the user does not provide any config path
const DefaultConfigPath string = "config.json"
const DefaultPrevConfigPath string = "prev_config.json"

// Below are the default values of the job schedules that would be used if the user does not provide any schedule or
// if there is any error while parsing the cron expression
const DefaultZTRCronExpr string = "@every 00h00m05s"
const DefaultFetchAndReplicateCronExpr string = "@every 00h00m30s"

// todo : this is just for testing, change later to 5m and 10m
const DefaultStateReportCronExpr string = "@every 00h00m10s"
const DefaultConfigReportCronExpr string = "@every 00h00m10s"

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

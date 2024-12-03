package config

// Job names that the user is expected to provide in the config.json file
const ReplicateStateJobName string = "replicate_state"
const UpdateConfigJobName string = "update_config"
const ZTRConfigJobName string = "register_satellite"

// The values below contain the default values of the constants used in the satellite. The user is allowed to override them
// by providing values in the config.json file. These default values will be used if the user does not provide any value or wrong format value
// in the config.json file.

// Default config.json path for the satellite, used if the user does not provide any config path
const DefaultConfigPath string = "config.json"
const DefaultZotConfigPath string = "./registry/config.json"

// Below are the default values of the job schedules that would be used if the user does not provide any schedule or
// if there is any error while parsing the cron expression
const DefaultFetchConfigFromGroundControlTimePeriod string = "@every 00h00m30s"
const DefaultZeroTouchRegistrationCronExpr string = "@every 00h00m05s"
const DefaultFetchAndReplicateStateTimePeriod string = "@every 00h00m10s"

const BringOwnRegistry bool = false

package config

import (
	"fmt"

	"github.com/robfig/cron/v3"
)

// JobConfig holds default schedule and validation settings for jobs.
type JobConfig struct {
	DefaultSchedule string
}

var essentialJobs = []string{ReplicateStateJobName, UpdateConfigJobName, ZTRConfigJobName}

// Predefined job configurations.
var jobConfigs = map[string]JobConfig{
	ReplicateStateJobName: {
		DefaultSchedule: DefaultFetchAndReplicateStateTimePeriod,
	},
	UpdateConfigJobName: {
		DefaultSchedule: DefaultFetchConfigFromGroundControlTimePeriod,
	},
	ZTRConfigJobName: {
		DefaultSchedule: DefaultZeroTouchRegistrationCronExpr,
	},
}

// ValidateCronJob validates a job's configuration, ensuring it has a valid schedule.
// It sets a default schedule if none is provided and returns warnings for any issues.
func ValidateCronJob(job *Job) (Warning, error) {
	config, exists := jobConfigs[job.Name]
	if !exists {
		return "", fmt.Errorf("unknown job name %s", job.Name)
	}

	warning := ensureSchedule(job, config.DefaultSchedule)
	if cronWarning := validateCronExpression(job.Schedule); cronWarning != "" {
		// Reset to default schedule if validation fails.
		job.Schedule = config.DefaultSchedule
		return cronWarning, nil
	}

	return warning, nil
}

// ensureSchedule ensures the job has a schedule, using the default if none is provided.
func ensureSchedule(job *Job, defaultSchedule string) Warning {
	if job.Schedule == "" {
		job.Schedule = defaultSchedule
		return Warning(fmt.Sprintf("no schedule provided for job %s, using default schedule %s", job.Name, defaultSchedule))
	}
	return ""
}

// validateCronExpression checks the validity of a cron expression.
func validateCronExpression(cronExpression string) Warning {

	if _, err := cron.ParseStandard(cronExpression); err != nil {
		return Warning(fmt.Sprintf("error parsing cron expression: %v", err))
	}
	return ""
}

// AddEssentialJobs adds essential jobs to the provided slice if they are not already present.
func AddEssentialJobs(jobsPresent *[]Job) {
	for _, jobName := range essentialJobs {
		found := false
		for _, job := range *jobsPresent {
			if job.Name == jobName {
				found = true
				break
			}
		}
		if !found {
			defaultSchedule := jobConfigs[jobName].DefaultSchedule
			*jobsPresent = append(*jobsPresent, Job{Name: jobName, Schedule: defaultSchedule})
		}
	}
}


func GetJobSchedule(jobName string) (string, error) {
	for _, job := range appConfig.LocalJsonConfig.Jobs {
		if job.Name == jobName {
			return job.Schedule, nil
		}
	}
	return "", fmt.Errorf("job %s not found", jobName)
}

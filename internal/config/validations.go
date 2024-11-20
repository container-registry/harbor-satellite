package config

import (
	"fmt"
	"regexp"
)

// Validation rules
var (
	numericPattern    = `^(?:[0-9]|[1-5][0-9])$|^[\*/,\-]+$`
	hoursPattern      = `^(?:[0-9]|1[0-9]|2[0-3])$|^[\*/,\-]+$`
	dayOfMonthPattern = `^(?:[1-9]|[12][0-9]|3[01])$|^[\*/,\-?]+$`
	monthPattern      = `^(?:[1-9]|1[0-2]|JAN|FEB|MAR|APR|MAY|JUN|JUL|AUG|SEP|OCT|NOV|DEC)$|^[\*/,\-]+$`
	dayOfWeekPattern  = `^(?:[0-6]|SUN|MON|TUE|WED|THU|FRI|SAT)$|^[\*/,\-?]+$`
	defaultValues     = map[string]string{
		"Seconds":    "0",
		"Minutes":    "0",
		"Hours":      "0",
		"DayOfMonth": "1",
		"Month":      "1",
		"DayOfWeek":  "0",
	}
)

// ValidateJobSchedule validates the job schedule fields and returns a list of errors
// if any of the fields are invalid as per the cron format rules for each field type
// Seconds: 0-59 or */,-,?
// Minutes: 0-59 or */,-,?
// Hours: 0-23 or */,-
// DayOfMonth: 1-31 or */,-,?
// Month: 1-12 or JAN-DEC or */,-
// DayOfWeek: 0-6 or SUN-SAT or */,-,?
// It would also update the job schedule fields with default values if any of the fields are invalid
func ValidateJobSchedule(job *Job) []string {
	var checks []string
	var warning string
	job.Seconds, warning = validateField(job.Seconds, numericPattern, "Seconds")
	if warning != "" {
		checks = append(checks, warning)
	}
	job.Minutes, warning = validateField(job.Minutes, numericPattern, "Minutes")
	if warning != "" {
		checks = append(checks, warning)
	}
	job.Hours, warning = validateField(job.Hours, hoursPattern, "Hours")
	if warning != "" {
		checks = append(checks, warning)
	}
	job.DayOfMonth, warning = validateField(job.DayOfMonth, dayOfMonthPattern, "DayOfMonth")
	if warning != "" {
		checks = append(checks, warning)
	}
	job.Month, warning = validateField(job.Month, monthPattern, "Month")
	if warning != "" {
		checks = append(checks, warning)
	}
	job.DayOfWeek, warning = validateField(job.DayOfWeek, dayOfWeekPattern, "DayOfWeek")
	if warning != "" {
		checks = append(checks, warning)
	}

	return checks
}

// Helper function for validation
func validateField(value, pattern, fieldName string) (string, string) {
	match, _ := regexp.MatchString(pattern, value)
	if !match {
		return defaultValues[fieldName], fmt.Sprintf("invalid value for %s: %s, using default value %s", fieldName, value, defaultValues[fieldName])
	}
	return value, ""
}

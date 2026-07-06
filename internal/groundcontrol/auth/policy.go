package auth

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

type PasswordPolicy struct {
	MinLength        int
	MaxLength        int
	RequireUppercase bool
	RequireLowercase bool
	RequireNumber    bool
	RequireSpecial   bool
}

func DefaultPolicy() PasswordPolicy {
	return PasswordPolicy{
		MinLength:        8,
		MaxLength:        128,
		RequireUppercase: true,
		RequireLowercase: true,
		RequireNumber:    true,
		RequireSpecial:   false,
	}
}

func LoadPolicyFromEnv() PasswordPolicy {
	policy := DefaultPolicy()

	if v := os.Getenv("PASSWORD_MIN_LENGTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			policy.MinLength = n
		}
	}

	if v := os.Getenv("PASSWORD_MAX_LENGTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			policy.MaxLength = n
		}
	}

	if v := os.Getenv("PASSWORD_REQUIRE_UPPERCASE"); v != "" {
		policy.RequireUppercase = parseBool(v)
	}

	if v := os.Getenv("PASSWORD_REQUIRE_LOWERCASE"); v != "" {
		policy.RequireLowercase = parseBool(v)
	}

	if v := os.Getenv("PASSWORD_REQUIRE_NUMBER"); v != "" {
		policy.RequireNumber = parseBool(v)
	}

	if v := os.Getenv("PASSWORD_REQUIRE_SPECIAL"); v != "" {
		policy.RequireSpecial = parseBool(v)
	}

	return policy
}

func (p PasswordPolicy) Validate(password string) error {
	var violations []string

	if len(password) < p.MinLength {
		violations = append(violations, fmt.Sprintf("at least %d characters", p.MinLength))
	}

	if len(password) > p.MaxLength {
		violations = append(violations, fmt.Sprintf("at most %d characters", p.MaxLength))
	}

	if p.RequireUppercase && !containsUppercase(password) {
		violations = append(violations, "one uppercase letter")
	}

	if p.RequireLowercase && !containsLowercase(password) {
		violations = append(violations, "one lowercase letter")
	}

	if p.RequireNumber && !containsNumber(password) {
		violations = append(violations, "one number")
	}

	if p.RequireSpecial && !containsSpecial(password) {
		violations = append(violations, "one special character")
	}

	if len(violations) == 0 {
		return nil
	}

	return fmt.Errorf("password must contain: %s", strings.Join(violations, ", "))
}

func containsUppercase(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func containsLowercase(s string) bool {
	for _, r := range s {
		if unicode.IsLower(r) {
			return true
		}
	}
	return false
}

func containsNumber(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsSpecial(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

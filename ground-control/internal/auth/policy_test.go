package auth

import (
	"testing"
)

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.MinLength != 8 {
		t.Errorf("expected MinLength 8, got %d", p.MinLength)
	}
	if p.MaxLength != 128 {
		t.Errorf("expected MaxLength 128, got %d", p.MaxLength)
	}
	if !p.RequireUppercase {
		t.Error("expected RequireUppercase to be true")
	}
	if !p.RequireLowercase {
		t.Error("expected RequireLowercase to be true")
	}
	if !p.RequireNumber {
		t.Error("expected RequireNumber to be true")
	}
	if p.RequireSpecial {
		t.Error("expected RequireSpecial to be false")
	}
}

func TestValidate_ValidPassword(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate("Abcdefg1"); err != nil {
		t.Errorf("expected valid password, got error: %v", err)
	}
}

func TestValidate_TooShort(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate("Ab1"); err == nil {
		t.Error("expected error for too short password")
	}
}

func TestValidate_TooLong(t *testing.T) {
	p := DefaultPolicy()
	long := make([]byte, 129)
	for i := range long {
		long[i] = 'a'
	}
	long[0] = 'A'
	long[1] = '1'
	if err := p.Validate(string(long)); err == nil {
		t.Error("expected error for too long password")
	}
}

func TestValidate_MissingUppercase(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate("abcdefg1"); err == nil {
		t.Error("expected error for missing uppercase")
	}
}

func TestValidate_MissingLowercase(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate("ABCDEFG1"); err == nil {
		t.Error("expected error for missing lowercase")
	}
}

func TestValidate_MissingNumber(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate("Abcdefgh"); err == nil {
		t.Error("expected error for missing number")
	}
}

func TestValidate_RequireSpecial(t *testing.T) {
	p := DefaultPolicy()
	p.RequireSpecial = true
	if err := p.Validate("Abcdefg1"); err == nil {
		t.Error("expected error for missing special character")
	}
	if err := p.Validate("Abcdefg1!"); err != nil {
		t.Errorf("expected valid password with special char, got: %v", err)
	}
}

func TestValidate_MultipleViolations(t *testing.T) {
	p := DefaultPolicy()
	err := p.Validate("abc")
	if err == nil {
		t.Error("expected error for multiple violations")
	}
}

func TestLoadPolicyFromEnv_Defaults(t *testing.T) {
	p := LoadPolicyFromEnv()
	if p.MinLength != 8 {
		t.Errorf("expected default MinLength 8, got %d", p.MinLength)
	}
	if p.MaxLength != 128 {
		t.Errorf("expected default MaxLength 128, got %d", p.MaxLength)
	}
}

func TestLoadPolicyFromEnv_Override(t *testing.T) {
	t.Setenv("PASSWORD_MIN_LENGTH", "12")
	t.Setenv("PASSWORD_MAX_LENGTH", "64")
	t.Setenv("PASSWORD_REQUIRE_SPECIAL", "true")
	t.Setenv("PASSWORD_REQUIRE_UPPERCASE", "false")

	p := LoadPolicyFromEnv()
	if p.MinLength != 12 {
		t.Errorf("expected MinLength 12, got %d", p.MinLength)
	}
	if p.MaxLength != 64 {
		t.Errorf("expected MaxLength 64, got %d", p.MaxLength)
	}
	if !p.RequireSpecial {
		t.Error("expected RequireSpecial true")
	}
	if p.RequireUppercase {
		t.Error("expected RequireUppercase false")
	}
}

func TestLoadPolicyFromEnv_InvalidValues(t *testing.T) {
	t.Setenv("PASSWORD_MIN_LENGTH", "notanumber")
	t.Setenv("PASSWORD_MAX_LENGTH", "-5")
	p := LoadPolicyFromEnv()
	if p.MinLength != 8 {
		t.Errorf("expected fallback MinLength 8, got %d", p.MinLength)
	}
	if p.MaxLength != 128 {
		t.Errorf("expected fallback MaxLength 128, got %d", p.MaxLength)
	}
}

func TestParseBool(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"TRUE", true},
		{"  yes  ", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"", false},
	}
	for _, tc := range cases {
		got := parseBool(tc.input)
		if got != tc.expected {
			t.Errorf("parseBool(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestContainsUppercase(t *testing.T) {
	if !containsUppercase("abcAbc") {
		t.Error("expected true")
	}
	if containsUppercase("abcabc") {
		t.Error("expected false")
	}
}

func TestContainsLowercase(t *testing.T) {
	if !containsLowercase("ABCabc") {
		t.Error("expected true")
	}
	if containsLowercase("ABCABC") {
		t.Error("expected false")
	}
}

func TestContainsNumber(t *testing.T) {
	if !containsNumber("abc1") {
		t.Error("expected true")
	}
	if containsNumber("abcabc") {
		t.Error("expected false")
	}
}

func TestContainsSpecial(t *testing.T) {
	if !containsSpecial("abc!") {
		t.Error("expected true")
	}
	if containsSpecial("abc123ABC") {
		t.Error("expected false")
	}
}

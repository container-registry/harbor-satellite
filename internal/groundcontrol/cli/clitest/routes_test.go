package clitest

import "testing"

func TestSameJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, printed, want string
		same                bool
	}{
		{"indented", "{\n  \"name\": \"x\"\n}\n", `{"name":"x"}`, true},
		{"extra field", `{"name":"x","token":"leak"}`, `{"name":"x"}`, false},
		{"extra output", "{\"name\":\"x\"}\ndone\n", `{"name":"x"}`, false},
		{"answer inside a larger array", `[{"name":"x"},{"name":"y"}]`, `{"name":"x"}`, false},
		{"altered string spacing", `{"name":"a b"}`, `{"name":"ab"}`, false},
		{"not JSON", "204 No Content\n", `{"name":"x"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SameJSON(tt.printed, tt.want); got != tt.same {
				t.Fatalf("SameJSON(%q, %q) = %v, want %v", tt.printed, tt.want, got, tt.same)
			}
		})
	}
}

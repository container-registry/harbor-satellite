package env

import (
	"testing"
)

func TestDatabaseURL(t *testing.T) {
	tests := []struct {
		name     string
		db       Database
		expected string
	}{
		{
			name: "standard credentials",
			db: Database{
				Username: "postgres",
				Password: "secretpassword",
				Host:     "localhost",
				Port:     "5432",
				Database: "harbor_satellite",
			},
			expected: "postgres://postgres:secretpassword@localhost:5432/harbor_satellite?sslmode=disable",
		},
		{
			name: "special characters in password",
			db: Database{
				Username: "gc_user",
				Password: "p@ss#word%123?",
				Host:     "127.0.0.1",
				Port:     "5432",
				Database: "groundcontrol",
			},
			expected: "postgres://gc_user:p%40ss%23word%25123%3F@127.0.0.1:5432/groundcontrol?sslmode=disable",
		},
		{
			name: "special characters in username",
			db: Database{
				Username: "user@domain.com",
				Password: "secret",
				Host:     "db.example.com",
				Port:     "5432",
				Database: "db",
			},
			expected: "postgres://user%40domain.com:secret@db.example.com:5432/db?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.db.URL()
			if got != tt.expected {
				t.Errorf("Database.URL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

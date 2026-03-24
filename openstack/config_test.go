package openstack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary directory
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.ini")

	// Test with non-existent file
	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for non-existent config file, got nil")
	}

	// Create a valid config file
	validINI := `
[DEFAULT]
ignore=true

[project1]
project_id=123
auth_url=http://auth
domain_name=domain
region_name=ru-1
username=user1
password=pass1
`
	if err := os.WriteFile(configPath, []byte(validINI), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	configs, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	c := configs[0]
	if c.ProjectName != "project1" {
		t.Errorf("expected ProjectName 'project1', got %q", c.ProjectName)
	}
	if c.ProjectID != "123" {
		t.Errorf("expected ProjectID '123', got %q", c.ProjectID)
	}
	if c.Username != "user1" {
		t.Errorf("expected Username 'user1', got %q", c.Username)
	}

	// Test with empty configs (only DEFAULT)
	emptyINI := `
[DEFAULT]
ignore=true
`
	if err := os.WriteFile(configPath, []byte(emptyINI), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for config with no projects, got nil")
	}

	// Test with invalid INI
	invalidINI := `
[project1
missing_bracket=true
`
	if err := os.WriteFile(configPath, []byte(invalidINI), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid INI file, got nil")
	}
}

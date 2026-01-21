package identity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-chronos/internal/identity"
)

func TestDefaultConfig(t *testing.T) {
	cfg := identity.DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig should be valid: %v", err)
	}
	if cfg.Author.Name == "" || cfg.Author.Email == "" {
		t.Error("DefaultConfig author should be non-empty")
	}
}

func TestWriteAndLoadTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.yml")

	if err := identity.WriteTemplate(path, false); err != nil {
		t.Fatalf("WriteTemplate: %v", err)
	}

	// File must exist
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("template file not created: %v", err)
	}

	// Should fail on second write without force
	if err := identity.WriteTemplate(path, false); err == nil {
		t.Error("expected error on duplicate write without --force")
	}

	// Should succeed with force
	if err := identity.WriteTemplate(path, true); err != nil {
		t.Errorf("WriteTemplate with force should succeed: %v", err)
	}
}

func TestLoadCustomYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yml")

	content := `
author:
  name: "Alice Dev"
  email: "alice@example.com"
committer:
  name: "Bob Ops"
  email: "bob@example.com"
gpg_sign: false
timezone: "Europe/Warsaw"
`
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := identity.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Author.Name != "Alice Dev" {
		t.Errorf("author name = %q, want %q", cfg.Author.Name, "Alice Dev")
	}
	if cfg.Committer.Email != "bob@example.com" {
		t.Errorf("committer email = %q, want %q", cfg.Committer.Email, "bob@example.com")
	}
	if cfg.Timezone != "Europe/Warsaw" {
		t.Errorf("timezone = %q, want %q", cfg.Timezone, "Europe/Warsaw")
	}
	loc := cfg.Location()
	if loc.String() != "Europe/Warsaw" {
		t.Errorf("Location() = %q, want %q", loc.String(), "Europe/Warsaw")
	}
}

func TestValidation_MissingFields(t *testing.T) {
	cfg := identity.Config{} // all empty
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for empty config")
	}
}

func TestValidation_BadTimezone(t *testing.T) {
	cfg := identity.DefaultConfig()
	cfg.Timezone = "Not/AReal/Zone"
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid timezone")
	}
}

func TestLoadFromRepo_CreatesTemplateWhenMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := identity.LoadFromRepo(dir)
	if err == nil {
		t.Fatal("expected error when identity.yml is missing")
	}
	// Template should now exist
	if _, statErr := os.Stat(filepath.Join(dir, identity.IdentityFileName)); statErr != nil {
		t.Errorf("template file was not created: %v", statErr)
	}
	// Error message should guide the user
	if !strings.Contains(err.Error(), "edit it with your details") {
		t.Errorf("error message not helpful: %v", err)
	}
}

func TestLoadFromRepo_LoadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	content := `
author:
  name: "Test Author"
  email: "author@test.com"
committer:
  name: "Test Committer"
  email: "committer@test.com"
gpg_sign: false
timezone: "UTC"
`
	os.WriteFile(filepath.Join(dir, identity.IdentityFileName), []byte(content), 0644)
	cfg, err := identity.LoadFromRepo(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Author.Name != "Test Author" {
		t.Errorf("author name = %q", cfg.Author.Name)
	}
}

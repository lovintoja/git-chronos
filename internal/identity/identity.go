package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level structure of the identity YAML file.
type Config struct {
	Author    Person `yaml:"author"`
	Committer Person `yaml:"committer"`
	GPGSign   bool   `yaml:"gpg_sign"`
	Timezone  string `yaml:"timezone"` // e.g. "Europe/Warsaw", defaults to UTC
}

// Person holds the identity fields for author or committer.
type Person struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

// DefaultConfig returns a Config populated with sensible placeholder values.
func DefaultConfig() Config {
	return Config{
		Author: Person{
			Name:  "Your Name",
			Email: "you@example.com",
		},
		Committer: Person{
			Name:  "Your Name",
			Email: "you@example.com",
		},
		GPGSign:  false,
		Timezone: "UTC",
	}
}

// DefaultTemplate is the YAML template written by the init-identity command.
// It includes comments explaining each field.
const DefaultTemplate = `# git-chronos identity configuration
# Used by: git-chronos rewrite --identity <this-file>
#
# author    - the person who originally wrote the code
# committer - the person who applied the commit (often the same as author)
# Both can be set independently.

author:
  name: "Your Name"
  email: "you@example.com"

committer:
  name: "Your Name"
  email: "you@example.com"

# Set to true to GPG-sign commits (requires gpg configured in git)
gpg_sign: false

# IANA timezone name for commit timestamps, e.g. "Europe/Warsaw", "America/New_York"
# Use "UTC" for universal time.
timezone: "UTC"
`

// Load reads and validates a Config from a YAML file at path.
// Returns an error with a clear message if the file is missing or malformed.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("identity file not found: %q\n  Run: git-chronos init-identity --output %s", path, path)
		}
		return nil, fmt.Errorf("reading identity file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing identity file %q: %w\n  Check YAML syntax and try again.", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid identity file %q: %w", path, err)
	}

	return &cfg, nil
}

// IdentityFileName is the fixed filename looked up in the repo root.
const IdentityFileName = "identity.yml"

// LoadFromRepo loads identity.yml from repoPath.
// If the file does not exist it is created from the default template
// and an error is returned instructing the user to fill it in.
func LoadFromRepo(repoPath string) (*Config, error) {
	path := filepath.Join(repoPath, IdentityFileName)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if writeErr := WriteTemplate(path, false); writeErr != nil {
			return nil, fmt.Errorf("could not create default identity file: %w", writeErr)
		}
		return nil, fmt.Errorf(
			"identity file not found — a default template has been created at:\n  %s\n\nPlease edit it with your details and re-run the command.",
			path,
		)
	}

	return Load(path)
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Author.Name) == "" {
		return fmt.Errorf("author.name must not be empty")
	}
	if strings.TrimSpace(c.Author.Email) == "" {
		return fmt.Errorf("author.email must not be empty")
	}
	if strings.TrimSpace(c.Committer.Name) == "" {
		return fmt.Errorf("committer.name must not be empty")
	}
	if strings.TrimSpace(c.Committer.Email) == "" {
		return fmt.Errorf("committer.email must not be empty")
	}
	if c.Timezone != "" {
		if _, err := time.LoadLocation(c.Timezone); err != nil {
			return fmt.Errorf("timezone %q is not a valid IANA timezone: %w", c.Timezone, err)
		}
	}
	return nil
}

// Location returns the parsed *time.Location for the config's timezone.
// Falls back to UTC on error.
func (c *Config) Location() *time.Location {
	if c.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// WriteTemplate writes DefaultTemplate to the given path.
// Returns an error if the file already exists (to avoid accidental overwrites).
func WriteTemplate(path string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file already exists: %q  (use --force to overwrite)", path)
		}
	}
	return os.WriteFile(path, []byte(DefaultTemplate), 0644)
}

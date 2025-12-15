package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SecurityConfig represents security configuration.
type SecurityConfig struct {
	Security struct {
		Auth struct {
			Provider string `yaml:"provider"`
			Basic    struct {
				MinPasswordLength int      `yaml:"min_password_length"`
				WeakPasswords     []string `yaml:"weak_passwords"`
			} `yaml:"basic"`
		} `yaml:"auth"`
		PublicEndpoints []string `yaml:"public_endpoints"`
		JWT             struct {
			SecretEnv   string `yaml:"secret_env"`
			ExpiryHours int    `yaml:"expiry_hours"`
		} `yaml:"jwt"`
	} `yaml:"security"`
}

// LoadSecurityConfig loads security configuration from YAML file.
// The path parameter is expected to come from a trusted source (command-line argument or hardcoded default).
func LoadSecurityConfig(path string) (*SecurityConfig, error) {
	// #nosec G304 -- path is provided by trusted source (CLI arg or config), not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config SecurityConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate required fields
	if err := validateSecurityConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// validateSecurityConfig validates the loaded configuration.
func validateSecurityConfig(config *SecurityConfig) error {
	// Validate auth provider
	if config.Security.Auth.Provider == "" {
		return fmt.Errorf("auth provider is required")
	}

	// Validate basic auth settings if provider is basic
	if config.Security.Auth.Provider == "basic" {
		if config.Security.Auth.Basic.MinPasswordLength <= 0 {
			return fmt.Errorf("min_password_length must be positive")
		}

		if config.Security.Auth.Basic.MinPasswordLength < 8 {
			return fmt.Errorf("min_password_length must be at least 8")
		}
	}

	// Validate JWT settings
	if config.Security.JWT.SecretEnv == "" {
		return fmt.Errorf("jwt secret_env is required")
	}

	if config.Security.JWT.ExpiryHours <= 0 {
		return fmt.Errorf("jwt expiry_hours must be positive")
	}

	return nil
}

// GetAuthProvider returns the configured authentication provider name.
func (c *SecurityConfig) GetAuthProvider() string {
	return c.Security.Auth.Provider
}

// GetMinPasswordLength returns the minimum password length requirement.
func (c *SecurityConfig) GetMinPasswordLength() int {
	return c.Security.Auth.Basic.MinPasswordLength
}

// GetWeakPasswords returns the list of weak passwords.
func (c *SecurityConfig) GetWeakPasswords() []string {
	return c.Security.Auth.Basic.WeakPasswords
}

// GetPublicEndpoints returns the list of public endpoints.
func (c *SecurityConfig) GetPublicEndpoints() []string {
	return c.Security.PublicEndpoints
}

// GetJWTSecretEnv returns the environment variable name for JWT secret.
func (c *SecurityConfig) GetJWTSecretEnv() string {
	return c.Security.JWT.SecretEnv
}

// GetJWTExpiryHours returns the JWT expiry time in hours.
func (c *SecurityConfig) GetJWTExpiryHours() int {
	return c.Security.JWT.ExpiryHours
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSecurityConfig(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "security-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name        string
		configYAML  string
		expectError bool
		errorMsg    string
		validate    func(*testing.T, *SecurityConfig)
	}{
		{
			name: "valid config",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 12
      weak_passwords:
        - "admin"
        - "password"
  public_endpoints:
    - "/health"
    - "/metrics"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 24
`,
			expectError: false,
			validate: func(t *testing.T, config *SecurityConfig) {
				if config.Security.Auth.Provider != "basic" {
					t.Errorf("expected provider 'basic', got '%s'", config.Security.Auth.Provider)
				}
				if config.Security.Auth.Basic.MinPasswordLength != 12 {
					t.Errorf("expected min_password_length 12, got %d", config.Security.Auth.Basic.MinPasswordLength)
				}
				if len(config.Security.Auth.Basic.WeakPasswords) != 2 {
					t.Errorf("expected 2 weak passwords, got %d", len(config.Security.Auth.Basic.WeakPasswords))
				}
				if len(config.Security.PublicEndpoints) != 2 {
					t.Errorf("expected 2 public endpoints, got %d", len(config.Security.PublicEndpoints))
				}
				if config.Security.JWT.SecretEnv != "JWT_SECRET" {
					t.Errorf("expected secret_env 'JWT_SECRET', got '%s'", config.Security.JWT.SecretEnv)
				}
				if config.Security.JWT.ExpiryHours != 24 {
					t.Errorf("expected expiry_hours 24, got %d", config.Security.JWT.ExpiryHours)
				}
			},
		},
		{
			name: "missing provider",
			configYAML: `security:
  auth:
    basic:
      min_password_length: 12
  public_endpoints:
    - "/health"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 24
`,
			expectError: true,
			errorMsg:    "auth provider is required",
		},
		{
			name: "zero min_password_length",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 0
  public_endpoints:
    - "/health"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 24
`,
			expectError: true,
			errorMsg:    "min_password_length must be positive",
		},
		{
			name: "min_password_length too short",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 6
  public_endpoints:
    - "/health"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 24
`,
			expectError: true,
			errorMsg:    "min_password_length must be at least 8",
		},
		{
			name: "missing jwt secret_env",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 12
  public_endpoints:
    - "/health"
  jwt:
    expiry_hours: 24
`,
			expectError: true,
			errorMsg:    "jwt secret_env is required",
		},
		{
			name: "zero jwt expiry_hours",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 12
  public_endpoints:
    - "/health"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 0
`,
			expectError: true,
			errorMsg:    "jwt expiry_hours must be positive",
		},
		{
			name: "negative jwt expiry_hours",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 12
  public_endpoints:
    - "/health"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: -1
`,
			expectError: true,
			errorMsg:    "jwt expiry_hours must be positive",
		},
		{
			name: "empty weak passwords",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 12
      weak_passwords: []
  public_endpoints:
    - "/health"
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 24
`,
			expectError: false,
			validate: func(t *testing.T, config *SecurityConfig) {
				if len(config.Security.Auth.Basic.WeakPasswords) != 0 {
					t.Errorf("expected 0 weak passwords, got %d", len(config.Security.Auth.Basic.WeakPasswords))
				}
			},
		},
		{
			name: "empty public endpoints",
			configYAML: `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 12
  public_endpoints: []
  jwt:
    secret_env: "JWT_SECRET"
    expiry_hours: 24
`,
			expectError: false,
			validate: func(t *testing.T, config *SecurityConfig) {
				if len(config.Security.PublicEndpoints) != 0 {
					t.Errorf("expected 0 public endpoints, got %d", len(config.Security.PublicEndpoints))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.configYAML), 0644); err != nil {
				t.Fatal(err)
			}

			// Load config
			config, err := LoadSecurityConfig(configPath)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errorMsg != "" && err.Error() != "config validation failed: "+tt.errorMsg {
					t.Errorf("expected error message containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
					return
				}

				if tt.validate != nil {
					tt.validate(t, config)
				}
			}
		})
	}
}

func TestLoadSecurityConfig_FileNotFound(t *testing.T) {
	_, err := LoadSecurityConfig("/nonexistent/path/config.yaml")

	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadSecurityConfig_InvalidYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configPath := filepath.Join(tmpDir, "invalid.yaml")
	invalidYAML := `
security:
  auth:
    provider: "basic"
    basic:
      min_password_length: invalid
`

	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = LoadSecurityConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestSecurityConfig_Getters(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "security-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configYAML := `security:
  auth:
    provider: "basic"
    basic:
      min_password_length: 15
      weak_passwords:
        - "admin"
        - "password"
        - "123456"
  public_endpoints:
    - "/health"
    - "/ready"
    - "/metrics"
  jwt:
    secret_env: "MY_JWT_SECRET"
    expiry_hours: 48
`

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := LoadSecurityConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Test GetAuthProvider
	if config.GetAuthProvider() != "basic" {
		t.Errorf("expected provider 'basic', got '%s'", config.GetAuthProvider())
	}

	// Test GetMinPasswordLength
	if config.GetMinPasswordLength() != 15 {
		t.Errorf("expected min password length 15, got %d", config.GetMinPasswordLength())
	}

	// Test GetWeakPasswords
	weakPasswords := config.GetWeakPasswords()
	if len(weakPasswords) != 3 {
		t.Errorf("expected 3 weak passwords, got %d", len(weakPasswords))
	}
	if weakPasswords[0] != "admin" {
		t.Errorf("expected first weak password to be 'admin', got '%s'", weakPasswords[0])
	}

	// Test GetPublicEndpoints
	publicEndpoints := config.GetPublicEndpoints()
	if len(publicEndpoints) != 3 {
		t.Errorf("expected 3 public endpoints, got %d", len(publicEndpoints))
	}
	if publicEndpoints[0] != "/health" {
		t.Errorf("expected first endpoint to be '/health', got '%s'", publicEndpoints[0])
	}

	// Test GetJWTSecretEnv
	if config.GetJWTSecretEnv() != "MY_JWT_SECRET" {
		t.Errorf("expected secret env 'MY_JWT_SECRET', got '%s'", config.GetJWTSecretEnv())
	}

	// Test GetJWTExpiryHours
	if config.GetJWTExpiryHours() != 48 {
		t.Errorf("expected expiry hours 48, got %d", config.GetJWTExpiryHours())
	}
}

func TestValidateSecurityConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *SecurityConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid basic provider",
			config: &SecurityConfig{
				Security: struct {
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
				}{
					Auth: struct {
						Provider string `yaml:"provider"`
						Basic    struct {
							MinPasswordLength int      `yaml:"min_password_length"`
							WeakPasswords     []string `yaml:"weak_passwords"`
						} `yaml:"basic"`
					}{
						Provider: "basic",
						Basic: struct {
							MinPasswordLength int      `yaml:"min_password_length"`
							WeakPasswords     []string `yaml:"weak_passwords"`
						}{
							MinPasswordLength: 12,
						},
					},
					JWT: struct {
						SecretEnv   string `yaml:"secret_env"`
						ExpiryHours int    `yaml:"expiry_hours"`
					}{
						SecretEnv:   "JWT_SECRET",
						ExpiryHours: 24,
					},
				},
			},
			expectError: false,
		},
		{
			name: "oauth provider (no basic validation)",
			config: &SecurityConfig{
				Security: struct {
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
				}{
					Auth: struct {
						Provider string `yaml:"provider"`
						Basic    struct {
							MinPasswordLength int      `yaml:"min_password_length"`
							WeakPasswords     []string `yaml:"weak_passwords"`
						} `yaml:"basic"`
					}{
						Provider: "oauth",
					},
					JWT: struct {
						SecretEnv   string `yaml:"secret_env"`
						ExpiryHours int    `yaml:"expiry_hours"`
					}{
						SecretEnv:   "JWT_SECRET",
						ExpiryHours: 24,
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSecurityConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
					return
				}
				if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

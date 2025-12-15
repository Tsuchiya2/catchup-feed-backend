package entity

import (
	"errors"
	"net"
	"testing"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid https URL",
			url:     "https://example.com/feed",
			wantErr: false,
		},
		{
			name:    "valid http URL",
			url:     "http://example.com/feed",
			wantErr: false,
		},
		{
			name:    "valid URL with port",
			url:     "https://example.com:8080/feed",
			wantErr: false,
		},
		{
			name:    "valid URL with query",
			url:     "https://example.com/feed?param=value",
			wantErr: false,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "invalid scheme - ftp",
			url:     "ftp://example.com/feed",
			wantErr: true,
		},
		{
			name:    "invalid scheme - file",
			url:     "file:///etc/passwd",
			wantErr: true,
		},
		{
			name:    "invalid scheme - javascript",
			url:     "javascript:alert(1)",
			wantErr: true,
		},
		{
			name:    "no host",
			url:     "https://",
			wantErr: true,
		},
		{
			name:    "malformed URL",
			url:     "ht!tp://example.com",
			wantErr: true,
		},
		{
			name:    "no scheme",
			url:     "example.com",
			wantErr: true,
		},
		{
			name:    "URL exceeding maximum length",
			url:     "https://example.com/" + string(make([]byte, 2050)),
			wantErr: true,
		},
		{
			name:    "localhost URL (private IP)",
			url:     "http://localhost/feed",
			wantErr: true,
		},
		{
			name:    "127.0.0.1 URL (loopback)",
			url:     "http://127.0.0.1/feed",
			wantErr: true,
		},
		{
			name:    "private IP 10.x.x.x",
			url:     "http://10.0.0.1/feed",
			wantErr: true,
		},
		{
			name:    "private IP 192.168.x.x",
			url:     "http://192.168.1.1/feed",
			wantErr: true,
		},
		{
			name:    "private IP 172.16.x.x",
			url:     "http://172.16.0.1/feed",
			wantErr: true,
		},
		{
			name:    "link-local 169.254.x.x (cloud metadata)",
			url:     "http://169.254.169.254/latest/meta-data",
			wantErr: true,
		},
		{
			name:    "valid URL with path and fragment",
			url:     "https://example.com/path/to/page#section",
			wantErr: false,
		},
		{
			name:    "valid URL with special characters in query",
			url:     "https://example.com/feed?q=test&sort=asc",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateURL_ErrorTypes(t *testing.T) {
	t.Run("empty URL returns ValidationError", func(t *testing.T) {
		err := ValidateURL("")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})

	t.Run("URL too long returns ValidationError", func(t *testing.T) {
		longURL := "https://example.com/" + string(make([]byte, 2050))
		err := ValidateURL(longURL)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})

	t.Run("invalid scheme returns ValidationError", func(t *testing.T) {
		err := ValidateURL("ftp://example.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})

	t.Run("missing host returns ValidationError", func(t *testing.T) {
		err := ValidateURL("https://")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})

	t.Run("private IP returns ValidationError", func(t *testing.T) {
		err := ValidateURL("http://127.0.0.1")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		isPrivate bool
	}{
		// Loopback addresses
		{
			name:      "IPv4 loopback 127.0.0.1",
			ip:        "127.0.0.1",
			isPrivate: true,
		},
		{
			name:      "IPv4 loopback 127.1.2.3",
			ip:        "127.1.2.3",
			isPrivate: true,
		},
		{
			name:      "IPv6 loopback ::1",
			ip:        "::1",
			isPrivate: true,
		},
		// Link-local addresses
		{
			name:      "IPv4 link-local 169.254.1.1",
			ip:        "169.254.1.1",
			isPrivate: true,
		},
		{
			name:      "IPv4 link-local 169.254.169.254 (AWS metadata)",
			ip:        "169.254.169.254",
			isPrivate: true,
		},
		{
			name:      "IPv6 link-local fe80::1",
			ip:        "fe80::1",
			isPrivate: true,
		},
		// Private IPv4 ranges
		{
			name:      "private 10.0.0.0/8 - start",
			ip:        "10.0.0.0",
			isPrivate: true,
		},
		{
			name:      "private 10.0.0.0/8 - middle",
			ip:        "10.123.45.67",
			isPrivate: true,
		},
		{
			name:      "private 10.0.0.0/8 - end",
			ip:        "10.255.255.255",
			isPrivate: true,
		},
		{
			name:      "private 172.16.0.0/12 - start",
			ip:        "172.16.0.0",
			isPrivate: true,
		},
		{
			name:      "private 172.16.0.0/12 - middle",
			ip:        "172.20.10.5",
			isPrivate: true,
		},
		{
			name:      "private 172.16.0.0/12 - end",
			ip:        "172.31.255.255",
			isPrivate: true,
		},
		{
			name:      "private 192.168.0.0/16 - start",
			ip:        "192.168.0.0",
			isPrivate: true,
		},
		{
			name:      "private 192.168.0.0/16 - middle",
			ip:        "192.168.1.1",
			isPrivate: true,
		},
		{
			name:      "private 192.168.0.0/16 - end",
			ip:        "192.168.255.255",
			isPrivate: true,
		},
		// Public IPs
		{
			name:      "public IP - Google DNS",
			ip:        "8.8.8.8",
			isPrivate: false,
		},
		{
			name:      "public IP - Cloudflare DNS",
			ip:        "1.1.1.1",
			isPrivate: false,
		},
		{
			name:      "public IP - example.com range",
			ip:        "93.184.216.34",
			isPrivate: false,
		},
		{
			name:      "public IPv6",
			ip:        "2001:4860:4860::8888",
			isPrivate: false,
		},
		// Edge cases near private ranges
		{
			name:      "just before 10.0.0.0/8",
			ip:        "9.255.255.255",
			isPrivate: false,
		},
		{
			name:      "just after 10.0.0.0/8",
			ip:        "11.0.0.0",
			isPrivate: false,
		},
		{
			name:      "just before 172.16.0.0/12",
			ip:        "172.15.255.255",
			isPrivate: false,
		},
		{
			name:      "just after 172.16.0.0/12",
			ip:        "172.32.0.0",
			isPrivate: false,
		},
		{
			name:      "just before 192.168.0.0/16",
			ip:        "192.167.255.255",
			isPrivate: false,
		},
		{
			name:      "just after 192.168.0.0/16",
			ip:        "192.169.0.0",
			isPrivate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}

			got := isPrivateIP(ip)
			if got != tt.isPrivate {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.isPrivate)
			}
		})
	}
}

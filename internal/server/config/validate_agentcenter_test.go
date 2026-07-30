package config

import "testing"

func acCfg(host, secret string) *Config {
	c := &Config{}
	c.Server.HTTP.Host = host
	c.Server.HTTP.Port = 8080
	c.Server.InternalSecret = secret
	return c
}

const strongSecret = "b3a1f0c9d8e7a6b5c4d3e2f1a0b9c8d7" // 32 hex chars

func TestValidateInternalSecret(t *testing.T) {
	cases := []struct {
		name, secret string
		wantErr      bool
	}{
		{"empty", "", true},
		{"blank", "   ", true},
		{"placeholder", "__INTERNAL_SECRET__", true},
		{"weak change-me", "change-me-but-quite-long-padding-xxxx", true},
		{"weak changeme", "supersafeCHANGEMEvaluepaddedlong1234", true},
		{"weak exact", "password", true},
		{"weak env example", "mxcwppInternalSecret789", true},
		{"too short 31", "b3a1f0c9d8e7a6b5c4d3e2f1a0b9c8d", true},
		{"exactly 32", strongSecret, false},
		{"long random", "0123456789abcdef0123456789abcdef0123456789", false},
	}
	for _, c := range cases {
		err := ValidateInternalSecret(c.secret)
		if c.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: expected nil, got %v", c.name, err)
		}
	}
}

// TestValidateAgentCenter_AlwaysRequiresStrongSecret loopback 与非 loopback 均要求强密钥，
// 空密钥一律 fail-fast（不允许 loopback 空密钥启动）。
func TestValidateAgentCenter_AlwaysRequiresStrongSecret(t *testing.T) {
	cases := []struct {
		name, host, secret string
		wantErr            bool
	}{
		{"loopback empty", "127.0.0.1", "", true},
		{"loopback localhost empty", "localhost", "", true},
		{"loopback ::1 empty", "::1", "", true},
		{"loopback weak", "127.0.0.1", "shortsecret", true},
		{"loopback strong", "127.0.0.1", strongSecret, false},
		{"wildcard empty", "0.0.0.0", "", true},
		{"wildcard placeholder", "0.0.0.0", "__INTERNAL_SECRET__", true},
		{"wildcard too short", "0.0.0.0", "shortsecret", true},
		{"wildcard weak default", "0.0.0.0", "change-me-please-generate-a-secret12", true},
		{"wildcard strong", "0.0.0.0", strongSecret, false},
		{"routable empty", "10.0.0.5", "", true},
		{"routable strong", "10.0.0.5", strongSecret, false},
		{"empty host defaults wildcard, empty secret", "", "", true},
	}
	for _, c := range cases {
		err := acCfg(c.host, c.secret).ValidateAgentCenter()
		if c.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: expected nil, got %v", c.name, err)
		}
	}
}

// TestValidateManager Manager 空/弱/强 internal_secret 的 fail-fast。
func TestValidateManager(t *testing.T) {
	cases := []struct {
		name, secret string
		wantErr      bool
	}{
		{"empty", "", true},
		{"weak", "changeme", true},
		{"short", "shortsecret", true},
		{"strong", strongSecret, false},
	}
	for _, c := range cases {
		err := acCfg("0.0.0.0", c.secret).ValidateManager()
		if c.wantErr && err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: expected nil, got %v", c.name, err)
		}
	}
}

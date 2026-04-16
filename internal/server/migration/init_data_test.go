package migration

import (
	"testing"

	"github.com/imkerbos/mxsec-platform/internal/server/config"
)

func TestBuildManagedPluginDownloadURL(t *testing.T) {
	if got := buildManagedPluginDownloadURL(nil, "collector"); got != "/api/v1/plugins/download/collector" {
		t.Fatalf("default download URL = %q", got)
	}

	cfg := &config.PluginsConfig{
		BaseURL: "http://manager:8080/api/v1/plugins/download/",
	}
	if got := buildManagedPluginDownloadURL(cfg, "collector"); got != "http://manager:8080/api/v1/plugins/download/collector" {
		t.Fatalf("base URL download = %q", got)
	}
}

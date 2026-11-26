package app

import (
	"path/filepath"
	"testing"
)

func TestPluginDirDefaults(t *testing.T) {
	tests := []struct{ name, cfgPath, dataDir, explicit, want string }{
		{"Entware", "/opt/etc/mounvix/config.json", "/data", "", "/opt/etc/mounvix/plugins"},
		{"Debian", "/etc/mounvix/config.json", "/data", "", "/etc/mounvix/plugins"},
		{"development", "/tmp/mounvix/config.json", "/data", "", filepath.Join("/data", "plugins")},
		{"explicit override", "/etc/mounvix/config.json", "/data", "/custom/plugins", "/custom/plugins"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{PluginDir: tt.explicit}
			if got := pluginDirForConfig(cfg, tt.cfgPath, tt.dataDir); got != tt.want {
				t.Fatalf("pluginDirForConfig()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicBasePathValidation(t *testing.T) {
	for _, path := range []string{"/mounvix", "/admin/ui", ""} {
		if err := validatePublicBasePath(path); err != nil {
			t.Errorf("valid path %q rejected: %v", path, err)
		}
	}
	for _, path := range []string{"mounvix", "//mounvix", "/mounvix/", "/a/../b", "/a\\b", "/%2e%2e/admin", "/a//b", "/?x=1"} {
		if err := validatePublicBasePath(path); err == nil {
			t.Errorf("unsafe path %q accepted", path)
		}
	}
}

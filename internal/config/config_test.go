package config

import "testing"

func TestLoadHostPublicIPLookupDisabledByDefault(t *testing.T) {
	t.Setenv("STACKLAB_HOST_PUBLIC_IP_LOOKUP_ENABLED", "")

	cfg := Load()
	if cfg.HostPublicIPLookupEnabled {
		t.Fatal("HostPublicIPLookupEnabled = true, want false by default")
	}
}

func TestLoadHostPublicIPLookupCanBeEnabled(t *testing.T) {
	t.Setenv("STACKLAB_HOST_PUBLIC_IP_LOOKUP_ENABLED", "true")

	cfg := Load()
	if !cfg.HostPublicIPLookupEnabled {
		t.Fatal("HostPublicIPLookupEnabled = false, want true")
	}
}

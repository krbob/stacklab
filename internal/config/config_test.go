package config

import (
	"net/netip"
	"testing"
)

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

func TestLoadTrustedProxiesParsesIPsAndCIDRs(t *testing.T) {
	t.Setenv("STACKLAB_TRUSTED_PROXIES", "10.0.0.0/8, 192.0.2.10, invalid, 2001:db8::/32")

	cfg := Load()
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.PrefixFrom(netip.MustParseAddr("192.0.2.10"), 32),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if len(cfg.TrustedProxies) != len(want) {
		t.Fatalf("len(TrustedProxies) = %d, want %d: %#v", len(cfg.TrustedProxies), len(want), cfg.TrustedProxies)
	}
	for index := range want {
		if cfg.TrustedProxies[index] != want[index] {
			t.Fatalf("TrustedProxies[%d] = %s, want %s", index, cfg.TrustedProxies[index], want[index])
		}
	}
}

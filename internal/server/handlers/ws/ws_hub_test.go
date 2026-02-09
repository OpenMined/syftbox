package ws

import "testing"

func TestAdvertisedIceServers_DefaultsToRequestHostAnd5349(t *testing.T) {
	t.Setenv("SYFTBOX_HOTLINK_ICE_SERVERS", "")
	t.Setenv("SYFTBOX_HOTLINK_TURN_HOST", "")
	t.Setenv("SYFTBOX_HOTLINK_TURN_PORT", "")

	got := advertisedIceServers("syftbox.example.com:8080")
	want := "turns:syftbox.example.com:5349?transport=tcp"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAdvertisedIceServers_UsesExplicitIceServersEnv(t *testing.T) {
	t.Setenv("SYFTBOX_HOTLINK_ICE_SERVERS", "turn:turn.example.com:3478?transport=udp")
	t.Setenv("SYFTBOX_HOTLINK_TURN_HOST", "ignored.example.com")
	t.Setenv("SYFTBOX_HOTLINK_TURN_PORT", "9999")

	got := advertisedIceServers("syftbox.example.com:8080")
	want := "turn:turn.example.com:3478?transport=udp"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAdvertisedIceServers_UsesHostAndPortOverrides(t *testing.T) {
	t.Setenv("SYFTBOX_HOTLINK_ICE_SERVERS", "")
	t.Setenv("SYFTBOX_HOTLINK_TURN_HOST", "turn.internal.example")
	t.Setenv("SYFTBOX_HOTLINK_TURN_PORT", "443")

	got := advertisedIceServers("syftbox.example.com:8080")
	want := "turns:turn.internal.example:443?transport=tcp"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

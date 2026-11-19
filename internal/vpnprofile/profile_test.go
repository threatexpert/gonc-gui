package vpnprofile

import (
	"encoding/json"
	"testing"
)

func TestDisconnectProtectionProfileRoundTrip(t *testing.T) {
	var legacy Profile
	if err := json.Unmarshal([]byte(`{"name":"Legacy"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.DisconnectProtection || legacy.ReconnectDNS != "" {
		t.Fatal(legacy)
	}
	want := DefaultProfile("Protected")
	want.DisconnectProtection, want.ReconnectDNS = true, "9.9.9.9:5353"
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Profile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%+v != %+v", got, want)
	}
}

func TestLANDiscoveryProfileCompatibility(t *testing.T) {
	if DefaultProfile("Test").P2PWithLAN {
		t.Fatal("new profiles must not enable LAN discovery by default")
	}
	var legacy Profile
	if err := json.Unmarshal([]byte(`{"name":"Legacy","passphrase":"test"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.P2PWithLAN {
		t.Fatal("legacy profiles must not enable LAN discovery")
	}
	profile := DefaultProfile("Test")
	profile.P2PWithLAN = true
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["p2pWithLan"] != true {
		t.Fatalf("exported p2pWithLan=%v, want true", fields["p2pWithLan"])
	}
	var restored Profile
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !restored.P2PWithLAN {
		t.Fatal("LAN discovery preference was lost on import")
	}
}

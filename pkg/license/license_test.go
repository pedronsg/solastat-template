package license

import (
	"os"
	"testing"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	privHex, err := os.ReadFile(os.Getenv("HOME") + "/.solastat/license-signing-key.hex")
	if err != nil {
		t.Skipf("no local signing key available: %v", err)
	}
	priv, err := ParsePrivateKeyHex(string(privHex))
	if err != nil {
		t.Fatalf("ParsePrivateKeyHex: %v", err)
	}

	key := Sign(priv, "device-serial-123", "relay")
	if !Verify("device-serial-123", "relay", key) {
		t.Fatal("expected valid signature to verify")
	}
	if Verify("device-serial-123", "gridcharge", key) {
		t.Fatal("signature for plugin 'relay' must not verify for plugin 'gridcharge'")
	}
	if Verify("other-device", "relay", key) {
		t.Fatal("signature for one device must not verify for another device")
	}
	if Verify("device-serial-123", "relay", "not-a-valid-signature") {
		t.Fatal("garbage license key must not verify")
	}
}

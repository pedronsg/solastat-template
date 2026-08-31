// Package license implements the offline Ed25519 signature scheme used to
// unlock a solastat plugin on a specific device.
//
// The message signed is always:
//
//	SHA256(deviceSerial + ":" + pluginID)
//
// The private key never leaves the developer's machine (see cmd/licensegen);
// only the public key is embedded here, in the public solastat-template repo
// — that's safe, Ed25519 public keys are meant to be shared.
package license

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// publicKeyHex is the Ed25519 public key that verifies every plugin license
// key issued for solastat. Generated once via `licensegen genkey`; the
// matching private key is kept only by the signer (pedronsg), never
// committed anywhere.
const publicKeyHex = "d811967e3b3f490b21066b5c255c37640660a93e5ac7f24a811210ddbc1678cc"

// PublicKey is the decoded form of publicKeyHex, used by Verify.
var PublicKey = mustDecodePublicKey(publicKeyHex)

func mustDecodePublicKey(hexKey string) ed25519.PublicKey {
	b, err := hex.DecodeString(hexKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		// Falls back to an all-zero key, against which Verify always fails —
		// safer than panicking or accepting a malformed key at import time.
		return make([]byte, ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b)
}

// Hash returns SHA256(deviceSerial + ":" + pluginID) as a lowercase hex
// string — this is what the Settings page displays for the user to copy
// and what the offline signer signs.
func Hash(deviceSerial, pluginID string) string {
	sum := sha256.Sum256([]byte(deviceSerial + ":" + pluginID))
	return hex.EncodeToString(sum[:])
}

// Verify reports whether licenseKey (base64-encoded Ed25519 signature) is a
// valid signature over Hash(deviceSerial, pluginID) under PublicKey.
func Verify(deviceSerial, pluginID, licenseKey string) bool {
	sig, err := base64.StdEncoding.DecodeString(licenseKey)
	if err != nil {
		return false
	}
	msg := []byte(Hash(deviceSerial, pluginID))
	return ed25519.Verify(PublicKey, msg, sig)
}

// Sign is only ever called offline by cmd/licensegen — never on a device —
// using the private key the operator holds locally.
func Sign(priv ed25519.PrivateKey, deviceSerial, pluginID string) string {
	return SignHash(priv, Hash(deviceSerial, pluginID))
}

// SignHash signs a hash string directly — used when the operator only has
// the hex hash copied from the Settings page (not the raw serial/pluginID).
func SignHash(priv ed25519.PrivateKey, hashHex string) string {
	sig := ed25519.Sign(priv, []byte(hashHex))
	return base64.StdEncoding.EncodeToString(sig)
}

// ParsePrivateKeyHex decodes a hex-encoded Ed25519 private key as produced
// by `licensegen -genkey`.
func ParsePrivateKeyHex(s string) (ed25519.PrivateKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(b))
	}
	return ed25519.PrivateKey(b), nil
}

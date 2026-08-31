// licensegen is an offline-only tool: it never runs on a device. It
// generates the Ed25519 key pair used to unlock solastat plugins, and signs
// the per-device, per-plugin hash shown on the Settings page.
//
// The resulting private key file must never be committed to any repo.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/pedronsg/solastat-template/pkg/license"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "genkey":
		genkeyCmd(os.Args[2:])
	case "sign":
		signCmd(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `licensegen — offline signer for solastat plugin licenses

Usage:
  licensegen genkey -out <privkey.hex>
      Generates a new Ed25519 key pair. Writes the private key (hex) to the
      given file and prints the public key (hex) to embed as
      license.PublicKey in pkg/license/license.go.

  licensegen sign -priv <privkey.hex> -serial <deviceSerial> -plugin <id>
  licensegen sign -priv <privkey.hex> -hash <hexHash>
      Signs the per-device/per-plugin hash and prints the license key
      (base64) to paste into the Settings page.`)
}

func genkeyCmd(args []string) {
	fs := flag.NewFlagSet("genkey", flag.ExitOnError)
	out := fs.String("out", "", "path to write the hex-encoded private key (required)")
	fs.Parse(args)

	if *out == "" {
		fmt.Fprintln(os.Stderr, "genkey: -out is required")
		os.Exit(2)
	}
	if _, err := os.Stat(*out); err == nil {
		fmt.Fprintf(os.Stderr, "genkey: %s already exists, refusing to overwrite\n", *out)
		os.Exit(1)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genkey: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*out, []byte(hex.EncodeToString(priv)), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "genkey: write private key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Private key written to %s — keep this file secret, never commit it.\n\n", *out)
	fmt.Printf("Public key (hex) — paste into pkg/license/license.go PublicKey:\n%s\n", hex.EncodeToString(pub))
}

func signCmd(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	privPath := fs.String("priv", "", "path to the hex-encoded private key file (required)")
	serial := fs.String("serial", "", "device serial (used with -plugin)")
	pluginID := fs.String("plugin", "", "plugin id, e.g. relay (used with -serial)")
	hashHex := fs.String("hash", "", "hex hash copied directly from the Settings page")
	fs.Parse(args)

	if *privPath == "" {
		fmt.Fprintln(os.Stderr, "sign: -priv is required")
		os.Exit(2)
	}
	rawPriv, err := os.ReadFile(*privPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: read private key: %v\n", err)
		os.Exit(1)
	}
	priv, err := license.ParsePrivateKeyHex(string(rawPriv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sign: %v\n", err)
		os.Exit(1)
	}

	var key string
	switch {
	case *hashHex != "":
		key = license.SignHash(priv, *hashHex)
	case *serial != "" && *pluginID != "":
		key = license.Sign(priv, *serial, *pluginID)
	default:
		fmt.Fprintln(os.Stderr, "sign: provide either -hash, or both -serial and -plugin")
		os.Exit(2)
	}

	fmt.Println(key)
}

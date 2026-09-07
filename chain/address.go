package chain

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
)

// addressPrefix is the human-readable tag on every address, the toy
// equivalent of a chain's bech32 prefix (cosmos1..., core1...).
const addressPrefix = "dyl"

// deriveAddress turns an ed25519 public key into its address.
func deriveAddress(publicKey ed25519.PublicKey) string {
	return addressPrefix + hex.EncodeToString(publicKey)
}

// AddressFromKey is the exported form of deriveAddress, for callers outside
// the package that hold a key pair (a faucet, a wallet).
func AddressFromKey(publicKey ed25519.PublicKey) string {
	return deriveAddress(publicKey)
}

// pubKeyFromAddress reverses deriveAddress, rejecting a missing prefix,
// invalid hex, or the wrong key length.
func pubKeyFromAddress(address string) (ed25519.PublicKey, error) {
	rest, found := strings.CutPrefix(address, addressPrefix)
	if !found {
		return nil, fmt.Errorf("address does not contain prefix %q", addressPrefix)
	}
	b, err := hex.DecodeString(rest)
	if err != nil {
		return nil, fmt.Errorf("cannot decode address: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("address decodes to %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

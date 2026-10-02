package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// catalogPrefix is prepended to the canonical catalog before signing. The
// core verifies catalog.sig over exactly these bytes.
const catalogPrefix = "ervisio-catalog-v1\n"

// Canonical returns the canonical form of a JSON document: decoded with
// UseNumber, re-encoded with sorted keys, no whitespace, no HTML escaping.
// It is a byte-for-byte copy of plugins.Canonical in the core
// (server/internal/modules/plugins/sign.go); keep them identical.
func Canonical(manifest []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(manifest))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil { // maps are encoded with sorted keys
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// catalogMessage is the exact byte string that is signed for a catalog.
func catalogMessage(catalog []byte) ([]byte, error) {
	c, err := Canonical(catalog)
	if err != nil {
		return nil, fmt.Errorf("catalog: %v", err)
	}
	return append([]byte(catalogPrefix), c...), nil
}

// SignCatalog returns the content of catalog.sig (base64 and a newline).
func SignCatalog(catalog []byte, key ed25519.PrivateKey) ([]byte, error) {
	msg, err := catalogMessage(catalog)
	if err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, msg)) + "\n"), nil
}

// VerifyCatalog checks a catalog.sig against one public key.
func VerifyCatalog(catalog, sig []byte, pub ed25519.PublicKey) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("catalog.sig is not a base64 ed25519 signature")
	}
	msg, err := catalogMessage(catalog)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, raw) {
		return errors.New("the catalog signature does not match the public key")
	}
	return nil
}

// ParsePrivate decodes a key file: the base64 of the 64-byte ed25519
// private key (what plugin-sign -genkey writes) or of the 32-byte seed.
func ParsePrivate(text string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, errors.New("the key is not base64")
	}
	switch len(b) {
	case ed25519.PrivateKeySize:
		k := ed25519.PrivateKey(b)
		if !k.Public().(ed25519.PublicKey).Equal(ed25519.NewKeyFromSeed(k.Seed()).Public()) {
			return nil, errors.New("corrupt private key")
		}
		return k, nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	}
	return nil, fmt.Errorf("expected a 32-byte seed or 64-byte private key, got %d bytes", len(b))
}

// LoadPrivate reads a key file, refusing files readable by group or others.
func LoadPrivate(path string) (ed25519.PrivateKey, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by other users (mode %v): chmod 600 it", path, fi.Mode().Perm())
	}
	if fi.Size() > 4<<10 {
		return nil, fmt.Errorf("%s is not a key file", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePrivate(string(b))
}

// ParsePublic decodes a base64 ed25519 public key.
func ParsePublic(text string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("not a base64 ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

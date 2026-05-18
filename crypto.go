package main

import (
	"crypto/cipher"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// SharedKey is a 32-byte hardcoded key.
// Both machines must have the exact same key.
// We will replace this with a DH handshake in the next step.
var SharedKey = [32]byte{
	0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x7a, 0x8b,
	0x9c, 0xad, 0xbe, 0xcf, 0xd0, 0xe1, 0xf2, 0x03,
	0x14, 0x25, 0x36, 0x47, 0x58, 0x69, 0x7a, 0x8b,
	0x9c, 0xad, 0xbe, 0xcf, 0xd0, 0xe1, 0xf2, 0x03,
}

// newCipher creates a ChaCha20-Poly1305 AEAD cipher from our shared key.
// AEAD = Authenticated Encryption with Associated Data.
// One cipher instance is reused for all packets.
func newCipher() cipher.AEAD {
	c, err := chacha20poly1305.New(SharedKey[:])
	if err != nil {
		panic(fmt.Sprintf("create cipher: %v", err))
	}
	return c
}

// Encrypt takes a raw IP packet and returns:
// [ 8 bytes nonce | ciphertext + 16 byte auth tag ]
//
// The nonce is derived from a counter that you pass in.
// Counter must never repeat for the same key.
func Encrypt(aead cipher.AEAD, counter uint64, plaintext []byte) []byte {
	// Build a 12-byte nonce (ChaCha20-Poly1305 requires exactly 12 bytes)
	// We put our 8-byte counter in the last 8 bytes, first 4 bytes are zero.
	nonce := make([]byte, aead.NonceSize()) // 12 bytes
	binary.BigEndian.PutUint64(nonce[4:], counter)

	// Seal encrypts and appends the auth tag.
	// dst=nonce means we prepend the nonce to the output.
	// So output = nonce (12 bytes) + ciphertext + tag (16 bytes)
	encrypted := aead.Seal(nonce, nonce, plaintext, nil)
	return encrypted
}

// Decrypt takes a received UDP payload and returns the original IP packet.
// It expects the format: [ 12 bytes nonce | ciphertext + 16 byte auth tag ]
func Decrypt(aead cipher.AEAD, data []byte) ([]byte, error) {
	nonceSize := aead.NonceSize() // 12 bytes

	if len(data) < nonceSize {
		return nil, fmt.Errorf("packet too short: %d bytes", len(data))
	}

	// Split nonce and ciphertext
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	// Open decrypts and verifies the auth tag.
	// If anyone tampered with the packet, this returns an error.
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (tampered or wrong key): %w", err)
	}

	return plaintext, nil
}
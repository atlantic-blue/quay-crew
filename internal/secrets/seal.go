package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// KeyLength is the size of the key that seals a secret, in bytes. Anything else is refused rather
// than padded or hashed into shape, because a key of the wrong length is somebody's mistake and
// quietly accepting it hides which secrets were sealed with what.
const KeyLength = 32

// Seal encrypts a value so that holding the database is not enough to read it. A database dump is a
// thing people paste into messages and attach to issues, and a token in one is a token somebody else
// has.
//
// The nonce is random per value and stored in front of the ciphertext, which is why setting the same
// secret twice produces different bytes.
func Seal(key []byte, value string) ([]byte, error) {
	sealer, err := sealer(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, sealer.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: nonce: %w", err)
	}
	return sealer.Seal(nonce, nonce, []byte(value), nil), nil
}

// Open decrypts a value sealed by Seal. A wrong key fails rather than returning something: the whole
// point is that the ciphertext is worth nothing on its own.
func Open(key, sealed []byte) (string, error) {
	opener, err := sealer(key)
	if err != nil {
		return "", err
	}
	if len(sealed) < opener.NonceSize() {
		return "", fmt.Errorf("secrets: sealed value is too short to be one")
	}
	nonce, body := sealed[:opener.NonceSize()], sealed[opener.NonceSize():]
	out, err := opener.Open(nil, nonce, body, nil)
	if err != nil {
		return "", fmt.Errorf("secrets: cannot open this value with this key: %w", err)
	}
	return string(out), nil
}

func sealer(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyLength {
		return nil, fmt.Errorf("secrets: a key is %d bytes, got %d", KeyLength, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: key: %w", err)
	}
	sealed, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher: %w", err)
	}
	return sealed, nil
}

// KeyAt reads the key that seals this crew's secrets, making one the first time.
//
// It lives on the host beside the data rather than in the database, so holding the database is not
// enough, and it is made rather than asked for because a step the operator has to perform before
// anything works is a step that gets skipped. It is written readable only by its owner.
func KeyAt(path string) ([]byte, error) {
	if raw, err := os.ReadFile(path); err == nil {
		key, err := hex.DecodeString(string(trimSpace(raw)))
		if err != nil {
			return nil, fmt.Errorf("secrets: the key at %s is not readable as one: %w", path, err)
		}
		if len(key) != KeyLength {
			return nil, fmt.Errorf("secrets: the key at %s is %d bytes, want %d", path, len(key), KeyLength)
		}
		return key, nil
	}

	key := make([]byte, KeyLength)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secrets: making a key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("secrets: making room for the key: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("secrets: writing the key: %w", err)
	}
	return key, nil
}

func trimSpace(raw []byte) []byte {
	for len(raw) > 0 && (raw[len(raw)-1] == '\n' || raw[len(raw)-1] == '\r' || raw[len(raw)-1] == ' ') {
		raw = raw[:len(raw)-1]
	}
	return raw
}

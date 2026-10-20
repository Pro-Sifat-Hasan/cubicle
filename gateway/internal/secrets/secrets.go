// Package secrets encrypts provider credentials at rest with AES-256-GCM.
// The data key lives in <home>/master.key; on Windows that file is additionally
// wrapped with DPAPI so it is useless if copied to another account or machine.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Pro-Sifat-Hasan/cubicle/gateway/internal/fsutil"
)

// NamePattern restricts secret names to environment-variable style names, since
// secrets are handed to the agent worker as environment variables.
var NamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)

type Box struct{ aead cipher.AEAD }

func Open(home string) (*Box, error) {
	path := filepath.Join(home, "master.key")
	var key []byte
	if wrapped, err := os.ReadFile(path); err == nil {
		key, err = unprotect(wrapped)
		if err != nil {
			return nil, fmt.Errorf("unlock master key: %w", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		wrapped, err := protect(key)
		if err != nil {
			return nil, fmt.Errorf("protect master key: %w", err)
		}
		if err := fsutil.WritePrivateFile(path, wrapped); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("master key has wrong length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext; name is bound as associated data so ciphertexts
// cannot be swapped between secret names.
func (b *Box) Seal(name string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, plaintext, []byte(name)), nil
}

func (b *Box) OpenSealed(name string, sealed []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return b.aead.Open(nil, sealed[:ns], sealed[ns:], []byte(name))
}

package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var ErrMissingKey = errors.New("missing secret key")

type Box struct {
	aead cipher.AEAD
}

func New(secret string) (*Box, error) {
	if secret == "" {
		return nil, ErrMissingKey
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (b *Box) Decrypt(ciphertext string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	nonceSize := b.aead.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("invalid ciphertext")
	}
	nonce := sealed[:nonceSize]
	body := sealed[nonceSize:]
	plain, err := b.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

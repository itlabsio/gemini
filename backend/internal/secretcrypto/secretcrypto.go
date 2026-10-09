// Package secretcrypto шифрует plain-пароли подключений перед записью в
// собственную БД метаданных (at rest). Ключ — SECRET_ENCRYPTION_KEY (32 байта).
package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// Box держит инициализированный AEAD.
type Box struct {
	aead cipher.AEAD
}

// New создаёт Box из 32-байтного ключа (AES-256-GCM).
func New(key string) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secretcrypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt возвращает nonce||ciphertext. Пустой вход даёт пустой выход.
func (b *Box) Encrypt(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt разбирает nonce||ciphertext обратно в строку.
func (b *Box) Decrypt(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	ns := b.aead.NonceSize()
	if len(data) < ns {
		return "", errors.New("secretcrypto: ciphertext too short")
	}
	nonce, ct := data[:ns], data[ns:]
	pt, err := b.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secretcrypto: decrypt failed: %w", err)
	}
	return string(pt), nil
}

package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// EnsureHeadIdentity гарантирует, что у головы есть Ed25519-ключ: при отсутствии
// строки генерирует пару, приватник шифрует (AES-256-GCM) и пишет. Идемпотентно
// и безопасно при гонке нескольких реплик (ON CONFLICT DO NOTHING).
// Возвращает публичный ключ головы.
func (s *Store) EnsureHeadIdentity(ctx context.Context) (ed25519.PublicKey, error) {
	pub, err := s.HeadPublicKey(ctx)
	if err == nil {
		return pub, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	genPub, genPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	encPriv, err := s.box.Encrypt(base64.StdEncoding.EncodeToString(genPriv))
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO head_identity (singleton, private_key, public_key)
		VALUES (TRUE, $1, $2)
		ON CONFLICT (singleton) DO NOTHING`,
		base64.StdEncoding.EncodeToString(encPriv),
		base64.StdEncoding.EncodeToString(genPub),
	); err != nil {
		return nil, err
	}
	return s.HeadPublicKey(ctx) // перечитываем — гонку могла выиграть другая реплика
}

// HeadPublicKey — публичный ключ головы (raw Ed25519).
func (s *Store) HeadPublicKey(ctx context.Context) (ed25519.PublicKey, error) {
	var b64 string
	if err := s.pool.QueryRow(ctx,
		`SELECT public_key FROM head_identity WHERE singleton`).Scan(&b64); err != nil {
		return nil, mapErr(err)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(raw), nil
}

// HeadPrivateKey — приватный ключ головы (расшифрованный). Только для подписи
// дампов на Source.
func (s *Store) HeadPrivateKey(ctx context.Context) (ed25519.PrivateKey, error) {
	var b64 string
	if err := s.pool.QueryRow(ctx,
		`SELECT private_key FROM head_identity WHERE singleton`).Scan(&b64); err != nil {
		return nil, mapErr(err)
	}
	enc, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	privB64, err := s.box.Decrypt(enc)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil {
		return nil, err
	}
	return ed25519.PrivateKey(raw), nil
}

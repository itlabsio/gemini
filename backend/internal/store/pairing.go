package store

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5" //nolint:revive // используется через тип pgx.Row

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

const pairingCols = `id, peer_role, peer_url, status, peer_public_key, established_at, created_at`

func scanPairing(row pgx.Row) (*model.HeadPairing, error) {
	var p model.HeadPairing
	err := row.Scan(&p.ID, &p.PeerRole, &p.PeerURL, &p.Status, &p.PeerPublicKey, &p.EstablishedAt, &p.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &p, nil
}

// CreatePairingCode (Source): создаёт pending-запись с хэшем одноразового кода и TTL.
func (s *Store) CreatePairingCode(ctx context.Context, peerRole, codeHash string, ttl time.Duration) (*model.HeadPairing, error) {
	return scanPairing(s.pool.QueryRow(ctx, `
		INSERT INTO head_pairings (peer_role, status, pairing_code_hash, code_expires_at)
		VALUES ($1, 'pending', $2, NOW() + make_interval(secs => $3))
		RETURNING `+pairingCols,
		peerRole, codeHash, ttl.Seconds()))
}

// PendingPairingByCodeHash находит непросроченную pending-запись по хэшу кода.
func (s *Store) PendingPairingByCodeHash(ctx context.Context, codeHash string) (*model.HeadPairing, error) {
	return scanPairing(s.pool.QueryRow(ctx, `
		SELECT `+pairingCols+` FROM head_pairings
		WHERE status = 'pending' AND pairing_code_hash = $1
		  AND code_expires_at IS NOT NULL AND code_expires_at > NOW()`,
		codeHash))
}

// encSecret шифрует HMAC-секрет пары перед записью (at rest).
func (s *Store) encSecret(secret string) (string, error) {
	b, err := s.box.Encrypt(secret)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// ActivatePairing (Source): переводит pending→active, сохраняет URL и публичный
// ключ пира и (зашифрованный) HMAC-секрет, гасит одноразовый код. Ревокается
// только прежняя active-запись с ТЕМ ЖЕ peer_url (перепэринг того же Target);
// другие Target'ы остаются — Source мультипировый.
func (s *Store) ActivatePairing(ctx context.Context, id, peerURL, secret, peerPubKey string) (*model.HeadPairing, error) {
	enc, err := s.encSecret(secret)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		UPDATE head_pairings SET status = 'revoked', auth_secret_hash = ''
		WHERE peer_role = (SELECT peer_role FROM head_pairings WHERE id = $1)
		  AND peer_url = $2 AND status = 'active' AND id <> $1`, id, peerURL); err != nil {
		return nil, err
	}
	p, err := scanPairing(tx.QueryRow(ctx, `
		UPDATE head_pairings
		SET status = 'active', peer_url = $2, auth_secret_hash = $3, peer_public_key = $4,
		    pairing_code_hash = '', code_expires_at = NULL, established_at = NOW()
		WHERE id = $1
		RETURNING `+pairingCols,
		id, peerURL, enc, peerPubKey))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// RevokePairing переводит active/pending-запись в revoked. Локально: пир об этом
// не уведомляется, но без общего секрета его подписанные запросы к нам (и наши к
// нему) сразу начинают отбиваться по HMAC, так что пара фактически разорвана.
// Чтобы связаться заново — нужен новый pairing с обеих сторон.
func (s *Store) RevokePairing(ctx context.Context, id string) (*model.HeadPairing, error) {
	return scanPairing(s.pool.QueryRow(ctx, `
		UPDATE head_pairings SET status = 'revoked', auth_secret_hash = '', peer_public_key = ''
		WHERE id = $1 AND status <> 'revoked'
		RETURNING `+pairingCols, id))
}

// UpsertActivePairingWithSource (DR): фиксирует active-запись про Source-пира.
// У Target ровно один Source — прежняя active-запись ревокается.
func (s *Store) UpsertActivePairingWithSource(ctx context.Context, peerURL, secret, peerPubKey string) (*model.HeadPairing, error) {
	enc, err := s.encSecret(secret)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx,
		`UPDATE head_pairings SET status = 'revoked', auth_secret_hash = '' WHERE peer_role = 'source' AND status = 'active'`); err != nil {
		return nil, err
	}
	p, err := scanPairing(tx.QueryRow(ctx, `
		INSERT INTO head_pairings (peer_role, peer_url, status, auth_secret_hash, peer_public_key, established_at)
		VALUES ('source', $1, 'active', $2, $3, NOW())
		RETURNING `+pairingCols, peerURL, enc, peerPubKey))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// ActivePairing возвращает одну active-запись роли пира (свежайшую). Source с
// несколькими Target'ами должен использовать ActivePairings.
func (s *Store) ActivePairing(ctx context.Context, peerRole string) (*model.HeadPairing, error) {
	return scanPairing(s.pool.QueryRow(ctx,
		`SELECT `+pairingCols+` FROM head_pairings WHERE peer_role = $1 AND status = 'active'
		 ORDER BY established_at DESC LIMIT 1`, peerRole))
}

// ActivePairings возвращает все active-записи роли пира (Source → несколько Target).
func (s *Store) ActivePairings(ctx context.Context, peerRole string) ([]model.HeadPairing, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+pairingCols+` FROM head_pairings WHERE peer_role = $1 AND status = 'active'
		 ORDER BY established_at`, peerRole)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.HeadPairing{}
	for rows.Next() {
		p, err := scanPairing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) decSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	return s.box.Decrypt(raw)
}

// PairingSecret отдаёт расшифрованный HMAC-секрет одной active-записи роли.
func (s *Store) PairingSecret(ctx context.Context, peerRole string) (string, error) {
	var enc string
	err := s.pool.QueryRow(ctx,
		`SELECT auth_secret_hash FROM head_pairings WHERE peer_role = $1 AND status = 'active'
		 ORDER BY established_at DESC LIMIT 1`, peerRole,
	).Scan(&enc)
	if err != nil {
		return "", mapErr(err)
	}
	return s.decSecret(enc)
}

// PairingSecretByID отдаёт расшифрованный HMAC-секрет конкретной пары.
func (s *Store) PairingSecretByID(ctx context.Context, id string) (string, error) {
	var enc string
	if err := s.pool.QueryRow(ctx,
		`SELECT auth_secret_hash FROM head_pairings WHERE id = $1`, id).Scan(&enc); err != nil {
		return "", mapErr(err)
	}
	return s.decSecret(enc)
}

// ActivePairingSecrets отдаёт секреты всех active-пар роли с их id — верификатор
// межголовых запросов перебирает их, чтобы принять запрос от любого из пиров.
func (s *Store) ActivePairingSecrets(ctx context.Context, peerRole string) ([]model.PairingSecret, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, auth_secret_hash FROM head_pairings WHERE peer_role = $1 AND status = 'active'`, peerRole)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PairingSecret
	for rows.Next() {
		var id, enc string
		if err := rows.Scan(&id, &enc); err != nil {
			return nil, err
		}
		sec, err := s.decSecret(enc)
		if err != nil {
			return nil, err
		}
		if sec != "" {
			out = append(out, model.PairingSecret{PairingID: id, Secret: sec})
		}
	}
	return out, rows.Err()
}

// PeerPublicKey отдаёт запиненный публичный ключ (raw Ed25519) одной active-пары
// роли — Target проверяет им подпись дампов Source.
func (s *Store) PeerPublicKey(ctx context.Context, peerRole string) ([]byte, error) {
	var b64 string
	err := s.pool.QueryRow(ctx,
		`SELECT peer_public_key FROM head_pairings WHERE peer_role = $1 AND status = 'active'
		 ORDER BY established_at DESC LIMIT 1`, peerRole,
	).Scan(&b64)
	if err != nil {
		return nil, mapErr(err)
	}
	if b64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(b64)
}

// LastPairingCodeIssuedAt возвращает время выдачи последнего ещё живого
// одноразового кода для роли пира (для рейт-лимита кнопки «Сгенерировать код»).
func (s *Store) LastPairingCodeIssuedAt(ctx context.Context, peerRole string) (time.Time, bool, error) {
	var t time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT created_at FROM head_pairings
		WHERE peer_role = $1 AND pairing_code_hash <> ''
		ORDER BY created_at DESC
		LIMIT 1`, peerRole).Scan(&t)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, err
	default:
		return t, true, nil
	}
}

// PurgeExpiredPendingPairings удаляет pending-записи, чей одноразовый код истёк,
// а обмена с пиром так и не произошло. Без этого «повисшие» pending копятся в
// списке связей и их нельзя убрать из UI.
func (s *Store) PurgeExpiredPendingPairings(ctx context.Context) (int64, error) {
	ct, err := s.pool.Exec(ctx, `
		DELETE FROM head_pairings
		WHERE status = 'pending'
		  AND code_expires_at IS NOT NULL
		  AND code_expires_at <= NOW()`)
	if err != nil {
		return 0, mapErr(err)
	}
	return ct.RowsAffected(), nil
}

// DeletePairing удаляет запись о связи. Активную связь удалять нельзя
// (ErrConflict) — она рвётся только перевязкой/ревокацией.
func (s *Store) DeletePairing(ctx context.Context, id string) error {
	var status string
	if err := s.pool.QueryRow(ctx,
		`SELECT status FROM head_pairings WHERE id = $1`, id).Scan(&status); err != nil {
		return mapErr(err)
	}
	if status == string(model.PairingActive) {
		return ErrConflict
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM head_pairings WHERE id = $1`, id)
	return mapErr(err)
}

// GetPairing возвращает одну запись пары по id.
func (s *Store) GetPairing(ctx context.Context, id string) (*model.HeadPairing, error) {
	return scanPairing(s.pool.QueryRow(ctx,
		`SELECT `+pairingCols+` FROM head_pairings WHERE id = $1`, id))
}

// ListPairings — для UI.
func (s *Store) ListPairings(ctx context.Context) ([]model.HeadPairing, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+pairingCols+` FROM head_pairings ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.HeadPairing{}
	for rows.Next() {
		p, err := scanPairing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

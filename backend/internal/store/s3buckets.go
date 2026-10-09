package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// S3BucketInput — данные для создания/обновления бакета.
type S3BucketInput struct {
	Name            string
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string // сырой ключ; шифруется здесь. Пусто при update — «не менять»
	PathStyle       bool
	UseSSL          bool
	Enabled         bool
}

// s3BucketCols — без зашифрованного секрета: он читается только EnabledS3Buckets.
const s3BucketCols = `id, name, endpoint, region, bucket, access_key_id,
	path_style, use_ssl, enabled, created_by, created_at, updated_at`

func scanS3Bucket(row pgx.Row, extra ...any) (*model.S3Bucket, error) {
	var b model.S3Bucket
	dst := append([]any{&b.ID, &b.Name, &b.Endpoint, &b.Region, &b.Bucket, &b.AccessKeyID,
		&b.PathStyle, &b.UseSSL, &b.Enabled, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt}, extra...)
	if err := row.Scan(dst...); err != nil {
		return nil, mapErr(err)
	}
	return &b, nil
}

// mapS3BucketErr — дубль имени бакета → ErrConflict.
func mapS3BucketErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrConflict
	}
	return mapErr(err)
}

// ListS3Buckets возвращает бакеты из БД (без секретов).
func (s *Store) ListS3Buckets(ctx context.Context) ([]model.S3Bucket, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+s3BucketCols+` FROM s3_buckets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.S3Bucket{}
	for rows.Next() {
		b, err := scanS3Bucket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// EnabledS3Buckets возвращает включённые бакеты с расшифрованными секретами
// (для storage.Pool и секрета кред Job'ов).
func (s *Store) EnabledS3Buckets(ctx context.Context) ([]model.S3Bucket, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+s3BucketCols+`, secret_access_key_encrypted
		FROM s3_buckets WHERE enabled ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.S3Bucket{}
	for rows.Next() {
		var enc []byte
		b, err := scanS3Bucket(rows, &enc)
		if err != nil {
			return nil, err
		}
		if b.SecretAccessKey, err = s.box.Decrypt(enc); err != nil {
			return nil, fmt.Errorf("decrypt s3 bucket %s secret: %w", b.Name, err)
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// GetS3Bucket возвращает один бакет (без секрета).
func (s *Store) GetS3Bucket(ctx context.Context, id string) (*model.S3Bucket, error) {
	return scanS3Bucket(s.pool.QueryRow(ctx, `SELECT `+s3BucketCols+` FROM s3_buckets WHERE id = $1`, id))
}

// CreateS3Bucket вставляет новый бакет.
func (s *Store) CreateS3Bucket(ctx context.Context, in S3BucketInput, createdBy string) (*model.S3Bucket, error) {
	enc, err := s.box.Encrypt(in.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt s3 secret: %w", err)
	}
	b, err := scanS3Bucket(s.pool.QueryRow(ctx, `
		INSERT INTO s3_buckets (name, endpoint, region, bucket, access_key_id,
			secret_access_key_encrypted, path_style, use_ssl, enabled, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING `+s3BucketCols,
		in.Name, in.Endpoint, in.Region, in.Bucket, in.AccessKeyID,
		enc, in.PathStyle, in.UseSSL, in.Enabled, createdBy))
	if err != nil {
		return nil, mapS3BucketErr(err)
	}
	return b, nil
}

// UpdateS3Bucket обновляет бакет. Пустой SecretAccessKey оставляет сохранённый.
func (s *Store) UpdateS3Bucket(ctx context.Context, id string, in S3BucketInput) (*model.S3Bucket, error) {
	var enc []byte
	if in.SecretAccessKey != "" {
		var err error
		if enc, err = s.box.Encrypt(in.SecretAccessKey); err != nil {
			return nil, fmt.Errorf("encrypt s3 secret: %w", err)
		}
	}
	b, err := scanS3Bucket(s.pool.QueryRow(ctx, `
		UPDATE s3_buckets SET name=$2, endpoint=$3, region=$4, bucket=$5, access_key_id=$6,
			secret_access_key_encrypted = COALESCE($7, secret_access_key_encrypted),
			path_style=$8, use_ssl=$9, enabled=$10
		WHERE id=$1
		RETURNING `+s3BucketCols,
		id, in.Name, in.Endpoint, in.Region, in.Bucket, in.AccessKeyID,
		enc, in.PathStyle, in.UseSSL, in.Enabled))
	if err != nil {
		return nil, mapS3BucketErr(err)
	}
	return b, nil
}

// DeleteS3Bucket удаляет бакет.
func (s *Store) DeleteS3Bucket(ctx context.Context, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM s3_buckets WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

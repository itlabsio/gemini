package store

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/notify"
)

// ErrNoSystemBot — канал использует системного бота, а тот не настроен.
var ErrNoSystemBot = errors.New("system bot is not configured")

// NotificationChannelInput — данные канала. Project задаётся отдельным аргументом.
type NotificationChannelInput struct {
	Name         string
	Provider     string
	ChatID       string
	ThreadID     string
	BotToken     string // сырой токен; шифруется здесь. Пусто при update — «не менять»
	BotName      string // имя бота из BotToken
	UseSystemBot bool
	OnFailure    bool
	OnSuccess    bool
	Enabled      bool
}

// notificationChannelCols — без токена: наружу отдаётся только факт его наличия.
const notificationChannelCols = `id, COALESCE(project, ''), name, provider,
	bot_token_encrypted IS NOT NULL, bot_name, chat_id, thread_id, on_failure, on_success, enabled, created_by, created_at`

// inScope — условие «канал принадлежит проекту $1» (” — глобальные каналы).
const inScope = `project IS NOT DISTINCT FROM NULLIF($1::text, '')`

func scanNotificationChannel(row pgx.Row) (*model.NotificationChannel, error) {
	var c model.NotificationChannel
	if err := row.Scan(&c.ID, &c.Project, &c.Name, &c.Provider, &c.HasOwnBot, &c.BotName, &c.ChatID, &c.ThreadID,
		&c.OnFailure, &c.OnSuccess, &c.Enabled, &c.CreatedBy, &c.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &c, nil
}

// mapChannelErr — несуществующий проект (нарушение FK) → ErrNotFound.
func mapChannelErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrNotFound
	}
	return err
}

// ListNotificationChannels возвращает каналы проекта (” — глобальные).
func (s *Store) ListNotificationChannels(ctx context.Context, project string) ([]model.NotificationChannel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+notificationChannelCols+`
		FROM notification_channels WHERE `+inScope+` ORDER BY name`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.NotificationChannel{}
	for rows.Next() {
		c, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetNotificationChannel возвращает канал только внутри указанного проекта.
func (s *Store) GetNotificationChannel(ctx context.Context, project, id string) (*model.NotificationChannel, error) {
	return scanNotificationChannel(s.pool.QueryRow(ctx, `SELECT `+notificationChannelCols+`
		FROM notification_channels WHERE `+inScope+` AND id::text = $2`, project, id))
}

// CreateNotificationChannel добавляет канал в проект (” — глобальный).
func (s *Store) CreateNotificationChannel(ctx context.Context, project string, in NotificationChannelInput, createdBy string) (*model.NotificationChannel, error) {
	enc, err := s.channelTokenEnc(in)
	if err != nil {
		return nil, err
	}
	c, err := scanNotificationChannel(s.pool.QueryRow(ctx, `
		INSERT INTO notification_channels (project, name, provider, bot_token_encrypted,
			chat_id, thread_id, on_failure, on_success, enabled, created_by, bot_name)
		VALUES (NULLIF($1::text, ''), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+notificationChannelCols,
		project, in.Name, in.Provider, enc, in.ChatID, in.ThreadID, in.OnFailure, in.OnSuccess, in.Enabled, createdBy, in.BotName))
	if err != nil {
		return nil, mapChannelErr(err)
	}
	return c, nil
}

// UpdateNotificationChannel обновляет канал. UseSystemBot сбрасывает свой токен,
// пустой BotToken без него оставляет сохранённый.
func (s *Store) UpdateNotificationChannel(ctx context.Context, project, id string, in NotificationChannelInput) (*model.NotificationChannel, error) {
	enc, err := s.channelTokenEnc(in)
	if err != nil {
		return nil, err
	}
	return scanNotificationChannel(s.pool.QueryRow(ctx, `
		UPDATE notification_channels SET name=$3, provider=$4,
			bot_token_encrypted = CASE WHEN $5::boolean THEN NULL ELSE COALESCE($6::bytea, bot_token_encrypted) END,
			bot_name = CASE WHEN $5::boolean THEN '' WHEN $12::text <> '' THEN $12::text ELSE bot_name END,
			chat_id=$7, thread_id=$8, on_failure=$9, on_success=$10, enabled=$11
		WHERE `+inScope+` AND id::text = $2
		RETURNING `+notificationChannelCols,
		project, id, in.Name, in.Provider, in.UseSystemBot, enc, in.ChatID, in.ThreadID, in.OnFailure, in.OnSuccess, in.Enabled, in.BotName))
}

func (s *Store) channelTokenEnc(in NotificationChannelInput) ([]byte, error) {
	if in.UseSystemBot || in.BotToken == "" {
		return nil, nil
	}
	enc, err := s.box.Encrypt(in.BotToken)
	if err != nil {
		return nil, fmt.Errorf("encrypt bot token: %w", err)
	}
	return enc, nil
}

// DeleteNotificationChannel удаляет канал только внутри указанного проекта.
func (s *Store) DeleteNotificationChannel(ctx context.Context, project, id string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM notification_channels WHERE `+inScope+` AND id::text = $2`, project, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// targetCols — канал с расшифровываемым токеном: своим либо системного бота.
const targetCols = `c.name, c.provider, COALESCE(c.bot_token_encrypted, b.token_encrypted), c.chat_id, c.thread_id
	FROM notification_channels c LEFT JOIN notification_bots b ON b.provider = c.provider`

func (s *Store) scanTarget(row pgx.Row) (notify.Channel, error) {
	var (
		ch  notify.Channel
		enc []byte
	)
	if err := row.Scan(&ch.Name, &ch.Provider, &enc, &ch.ChatID, &ch.ThreadID); err != nil {
		return ch, mapErr(err)
	}
	if enc == nil {
		return ch, ErrNoSystemBot
	}
	var err error
	if ch.Token, err = s.box.Decrypt(enc); err != nil {
		return ch, fmt.Errorf("decrypt bot token: %w", err)
	}
	return ch, nil
}

// NotificationTargets — включённые каналы для события: глобальные и каналы проекта.
// Канал, для которого нет рабочего токена, пропускается, чтобы не мешать остальным.
func (s *Store) NotificationTargets(ctx context.Context, project string, failure bool) ([]notify.Channel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+targetCols+`
		WHERE c.enabled AND (c.project IS NULL OR c.project = $1)
		  AND CASE WHEN $2::boolean THEN c.on_failure ELSE c.on_success END`, project, failure)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notify.Channel{}
	for rows.Next() {
		ch, err := s.scanTarget(rows)
		if err != nil {
			log.Printf("notify: channel %q skipped: %v", ch.Name, err)
			continue
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

// NotificationTarget — канал с токеном для тестовой отправки (внутри проекта).
func (s *Store) NotificationTarget(ctx context.Context, project, id string) (notify.Channel, error) {
	return s.scanTarget(s.pool.QueryRow(ctx, `SELECT `+targetCols+`
		WHERE c.project IS NOT DISTINCT FROM NULLIF($1::text, '') AND c.id::text = $2`, project, id))
}

// SystemBots — настроенные системные боты: провайдер → имя бота (без токенов).
func (s *Store) SystemBots(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT provider, bot_name FROM notification_bots`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var p, name string
		if err := rows.Scan(&p, &name); err != nil {
			return nil, err
		}
		out[p] = name
	}
	return out, rows.Err()
}

// SetSystemBot сохраняет токен системного бота провайдера.
func (s *Store) SetSystemBot(ctx context.Context, provider, token, botName, updatedBy string) error {
	enc, err := s.box.Encrypt(token)
	if err != nil {
		return fmt.Errorf("encrypt bot token: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO notification_bots (provider, token_encrypted, bot_name, updated_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider) DO UPDATE
		SET token_encrypted = EXCLUDED.token_encrypted, bot_name = EXCLUDED.bot_name,
		    updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		provider, enc, botName, updatedBy)
	return err
}

// DeleteSystemBot удаляет системного бота провайдера.
func (s *Store) DeleteSystemBot(ctx context.Context, provider string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM notification_bots WHERE provider = $1`, provider)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

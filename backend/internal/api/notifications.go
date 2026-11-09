package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/notify"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// channelScope — проект из пути (” — глобальные каналы) и проверка роли admin в нём.
// Identity.Can для ” пропускает только superuser.
func (s *Server) channelScope(w http.ResponseWriter, r *http.Request) (string, bool) {
	project := mux.Vars(r)["name"]
	if !identity(r).Can(project, auth.RoleAdmin) {
		writeForbidden(w, project, auth.RoleAdmin)
		return "", false
	}
	return project, true
}

type notificationsResponse struct {
	Channels   []model.NotificationChannel `json:"channels"`
	SystemBots map[string]string           `json:"system_bots"` // провайдер → имя бота
}

func (s *Server) listNotificationChannels(w http.ResponseWriter, r *http.Request) {
	project, ok := s.channelScope(w, r)
	if !ok {
		return
	}
	channels, err := s.d.Store.ListNotificationChannels(r.Context(), project)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	bots, err := s.d.Store.SystemBots(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notificationsResponse{Channels: channels, SystemBots: bots})
}

type notificationChannelRequest struct {
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	ChatID       string `json:"chat_id"`
	ThreadID     string `json:"thread_id"`
	BotToken     string `json:"bot_token"`
	UseSystemBot bool   `json:"use_system_bot"`
	OnFailure    bool   `json:"on_failure"`
	OnSuccess    bool   `json:"on_success"`
	Enabled      bool   `json:"enabled"`
}

func (req notificationChannelRequest) input() (store.NotificationChannelInput, string) {
	in := store.NotificationChannelInput{
		Name:         strings.TrimSpace(req.Name),
		Provider:     req.Provider,
		ChatID:       strings.TrimSpace(req.ChatID),
		ThreadID:     strings.TrimSpace(req.ThreadID),
		BotToken:     strings.TrimSpace(req.BotToken),
		UseSystemBot: req.UseSystemBot,
		OnFailure:    req.OnFailure,
		OnSuccess:    req.OnSuccess,
		Enabled:      req.Enabled,
	}
	if in.Provider == "" {
		in.Provider = notify.ProviderTelegram
	}
	in.BotToken = notify.NormalizeToken(in.Provider, in.BotToken)
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 64:
		return in, "name must be 1-64 characters"
	case !notify.ValidTarget(in.Provider, in.ChatID, in.ThreadID):
		return in, "unsupported provider or invalid chat_id / thread_id"
	case !in.UseSystemBot && in.BotToken != "" && !notify.ValidToken(in.Provider, in.BotToken):
		return in, "invalid bot_token format"
	}
	return in, ""
}

// checkBot — у канала должен быть бот, иначе он молча ничего не отправит.
func (s *Server) checkBot(w http.ResponseWriter, r *http.Request, in store.NotificationChannelInput, hasOwnBot bool) bool {
	if !in.UseSystemBot {
		if in.BotToken == "" && !hasOwnBot {
			writeError(w, http.StatusBadRequest, "bot_token is required unless use_system_bot is set")
			return false
		}
		return true
	}
	bots, err := s.d.Store.SystemBots(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return false
	}
	if _, ok := bots[in.Provider]; !ok {
		writeError(w, http.StatusBadRequest, "system bot is not configured for provider "+in.Provider)
		return false
	}
	return true
}

// botName запрашивает имя бота у мессенджера и заодно проверяет токен.
// При ошибке сам пишет ответ и возвращает ok=false.
func (s *Server) botName(w http.ResponseWriter, r *http.Request, provider, token string) (string, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	name, err := s.d.Notifier.BotName(ctx, provider, token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bot token check failed: "+err.Error())
		return "", false
	}
	return name, true
}

// fillBotName проверяет новый токен канала; без нового токена ничего не делает.
func (s *Server) fillBotName(w http.ResponseWriter, r *http.Request, in *store.NotificationChannelInput) bool {
	if in.UseSystemBot || in.BotToken == "" {
		return true
	}
	var ok bool
	in.BotName, ok = s.botName(w, r, in.Provider, in.BotToken)
	return ok
}

func (s *Server) createNotificationChannel(w http.ResponseWriter, r *http.Request) {
	project, ok := s.channelScope(w, r)
	if !ok {
		return
	}
	var req notificationChannelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, msg := req.input()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if !s.checkBot(w, r, in, false) || !s.fillBotName(w, r, &in) {
		return
	}
	by, _ := ctxIdentity(r)
	c, err := s.d.Store.CreateNotificationChannel(r.Context(), project, in, by)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "notification_channel.create", c.ID, channelAuditDetail(c))
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	project, ok := s.channelScope(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	var req notificationChannelRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, msg := req.input()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	cur, err := s.d.Store.GetNotificationChannel(r.Context(), project, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Токен одного провайдера другому не подходит: при смене нужен новый.
	if !s.checkBot(w, r, in, cur.HasOwnBot && cur.Provider == in.Provider) || !s.fillBotName(w, r, &in) {
		return
	}
	c, err := s.d.Store.UpdateNotificationChannel(r.Context(), project, id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "notification_channel.update", id, channelAuditDetail(c))
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	project, ok := s.channelScope(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	c, err := s.d.Store.GetNotificationChannel(r.Context(), project, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.d.Store.DeleteNotificationChannel(r.Context(), project, id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "notification_channel.delete", id, channelAuditDetail(c))
	w.WriteHeader(http.StatusNoContent)
}

// testNotificationChannel шлёт тестовое сообщение и возвращает ответ провайдера:
// иначе неверный chat_id обнаружится только при реальном провале бэкапа.
func (s *Server) testNotificationChannel(w http.ResponseWriter, r *http.Request) {
	project, ok := s.channelScope(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	ch, err := s.d.Store.NotificationTarget(r.Context(), project, id)
	if errors.Is(err, store.ErrNoSystemBot) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.d.Notifier.AllowTest(id) {
		writeError(w, http.StatusTooManyRequests, "test message was sent recently, try again in a few seconds")
		return
	}
	s.audit(r, "notification_channel.test", id, map[string]any{"name": ch.Name, "project": project, "chat_id": ch.ChatID})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	text := fmt.Sprintf("🔔 Gemini/%s: тестовое сообщение канала «%s»", s.d.Cfg.Role, ch.Name)
	if err := s.d.Notifier.Send(ctx, ch, text); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func channelAuditDetail(c *model.NotificationChannel) map[string]any {
	return map[string]any{"name": c.Name, "project": c.Project, "provider": c.Provider,
		"chat_id": c.ChatID, "enabled": c.Enabled, "own_bot": c.HasOwnBot, "bot_name": c.BotName}
}

type systemBotRequest struct {
	Token string `json:"token"`
}

func (s *Server) putSystemBot(w http.ResponseWriter, r *http.Request) {
	provider := mux.Vars(r)["provider"]
	var req systemBotRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	token := notify.NormalizeToken(provider, strings.TrimSpace(req.Token))
	if !notify.ValidToken(provider, token) {
		writeError(w, http.StatusBadRequest, "unsupported provider or invalid token format")
		return
	}
	name, ok := s.botName(w, r, provider, token)
	if !ok {
		return
	}
	by, _ := ctxIdentity(r)
	if err := s.d.Store.SetSystemBot(r.Context(), provider, token, name, by); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "notification_bot.update", provider, map[string]any{"name": provider, "bot_name": name})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteSystemBot(w http.ResponseWriter, r *http.Request) {
	provider := mux.Vars(r)["provider"]
	if err := s.d.Store.DeleteSystemBot(r.Context(), provider); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "notification_bot.delete", provider, map[string]any{"name": provider})
	w.WriteHeader(http.StatusNoContent)
}

package api

import (
	"errors"
	"net/http"
	"strings"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
)

var errVaultDisabled = errors.New("vault integration is disabled on this head")

// ctxIdentity возвращает человекочитаемого инициатора запроса ("" если не установлен).
func ctxIdentity(r *http.Request) (string, bool) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		return "", false
	}
	if id.Email != "" {
		return id.Email, true
	}
	return id.Subject, true
}

// validCron грубо проверяет, что в cron-выражении ровно 5 полей.
func validCron(expr string) bool {
	return len(strings.Fields(strings.TrimSpace(expr))) == 5
}

// keyUnderPrefix проверяет, что ключ S3 лежит строго внутри префикса сопоставления.
// Нужно, чтобы Source (или тот, кто добыл HMAC-секрет пары) не мог попросить
// восстановить в целевую базу произвольный объект бакета — в т.ч. дамп чужого
// сопоставления. Сегменты ".." отсекаются: Go нормализует путь при сборке URL,
// поэтому иначе ключ мог бы выйти за пределы префикса уже на стороне S3-клиента.
func keyUnderPrefix(key, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" || key == "" {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == ".." {
			return false
		}
	}
	return strings.HasPrefix(key, prefix+"/")
}

// firstField возвращает первое слово строки (напр. hex из "<hex>  db.dump").
func firstField(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

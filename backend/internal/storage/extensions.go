package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
)

// NoExtensionsDigest — digest отсутствующего <key>.extensions (у базы нет
// расширений, sidecar не заливался). Отличается от digest'а пустого файла:
// подпись фиксирует и сам факт отсутствия.
const NoExtensionsDigest = "none"

// maxExtensionsSize — потолок <key>.extensions: имён не больше пары сотен.
const maxExtensionsSize = 64 << 10

// ExtensionsDigest — то, что Source подписывает за <key>.extensions:
// "sha256:<hex>" содержимого либо NoExtensionsDigest, если файла нет.
func ExtensionsDigest(raw []byte, present bool) string {
	if !present {
		return NoExtensionsDigest
	}
	h := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(h[:])
}

// ValidExtName — консервативная проверка имени расширения (уходит в SQL restore'а).
func ValidExtName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		ok := r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// ParseExtensions разбирает <key>.extensions (по имени в строке), отбрасывая
// пустые строки и невалидные имена.
func ParseExtensions(raw []byte) []string {
	var names []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if line = strings.TrimSpace(line); line != "" && ValidExtName(line) {
			names = append(names, line)
		}
	}
	return names
}

// ReadExtensions скачивает <key>.extensions. present=false — файла нет (у
// базы нет расширений или дамп снят до появления sidecar'а).
func (c *Client) ReadExtensions(ctx context.Context, dumpKey string) (raw []byte, present bool, err error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, dumpKey+ExtensionsSuffix, minio.GetObjectOptions{})
	if err != nil {
		return nil, false, err
	}
	defer obj.Close()
	raw, err = io.ReadAll(io.LimitReader(obj, maxExtensionsSize))
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, false, nil
		}
		return nil, false, err
	}
	return raw, true, nil
}

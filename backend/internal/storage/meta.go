package storage

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
)

// DumpSuffix — расширение объекта дампа.
const DumpSuffix = ".dump"

// InstanceMetaKey — ключ подписанного Source файла <инстанс>/meta.json: из него
// Target при оффлайн-паре узнаёт проект инстанса. Лежит уровнем выше префиксов
// баз (<инстанс>/<база>/), под дамп не попадает.
func InstanceMetaKey(instance string) string { return instance + "/meta.json" }

// metaLimit — потолок размера meta.json и его подписи при чтении.
const metaLimit = 64 << 10

// ReadObject читает небольшой объект целиком (не больше metaLimit байт).
func (c *Client) ReadObject(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(io.LimitReader(obj, metaLimit))
}

// PutObject кладёт небольшой объект.
func (c *Client) PutObject(ctx context.Context, key, contentType string, body []byte) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Instances — имена инстансов: «каталоги» верхнего уровня бакета
// (<инстанс>/...). Один нерекурсивный листинг корня.
func (c *Client) Instances(ctx context.Context) ([]string, error) {
	var out []string
	for info := range c.mc.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{}) {
		if info.Err != nil {
			return nil, info.Err
		}
		if name, ok := strings.CutSuffix(info.Key, "/"); ok && name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// SignedDumpPrefixes — префиксы баз (<инстанс>/<база>) инстанса, под которыми
// лежит хотя бы один готовый дамп с .sig. Только по наличию файлов: подпись
// проверяет restore (operator.VerifiedDump). Рекурсивный листинг <инстанс>/.
func (c *Client) SignedDumpPrefixes(ctx context.Context, instance string) (map[string]bool, error) {
	var keys []string
	opts := minio.ListObjectsOptions{Prefix: instance + "/", Recursive: true}
	for info := range c.mc.ListObjects(ctx, c.bucket, opts) {
		if info.Err != nil {
			return nil, info.Err
		}
		keys = append(keys, info.Key)
	}
	return signedDumpPrefixes(keys), nil
}

func signedDumpPrefixes(keys []string) map[string]bool {
	have := make(map[string]bool, len(keys))
	for _, k := range keys {
		have[k] = true
	}
	out := map[string]bool{}
	for _, k := range keys {
		// Префикс — всё до последнего "/": имя базы само может содержать "/".
		i := strings.LastIndex(k, "/")
		if strings.HasSuffix(k, DumpSuffix) && i > 0 && strings.Contains(k[:i], "/") &&
			have[k+ChecksumSuffix] && have[k+SignatureSuffix] {
			out[k[:i]] = true
		}
	}
	return out
}

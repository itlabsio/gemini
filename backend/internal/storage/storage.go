// Package storage — абстракция промежуточного S3-хранилища дампов.
//
// Признак готовности дампа — отдельный объект "<key>.sha256" рядом с дампом:
// он заливается последним, только после полной записи дампа, и служит
// одновременно сигналом готовности и контрольной суммой.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// ChecksumSuffix добавляется к ключу дампа для файла контрольной суммы.
const ChecksumSuffix = ".sha256"

// SignatureSuffix — Ed25519-подпись манифеста дампа (ключ + sha256 + digest
// <key>.extensions) приватным
// ключом Source. Кладёт Source-голова (не Job — приватник в подах не появляется),
// Target проверяет запиненным публичным ключом. См. peer.SignArtifact.
const SignatureSuffix = ".sig"

// ExtensionsSuffix — список расширений исходной базы (по одному в строке), кладёт
// dump-Job. Покрыт подписью Source: Target применяет список, только сверив его
// digest (см. peer.SignArtifact), и передаёт restore-Job'у через env.
const ExtensionsSuffix = ".extensions"

// Client — обёртка над minio-go под конкретный бакет.
type Client struct {
	mc           *minio.Client
	id           string
	title        string
	name         string
	endpoint     string
	bucket       string
	downloadable bool
}

// bucketLabel — как бакет показывается в логах и нотификациях: s3://bucket (endpoint).
func bucketLabel(endpoint, bucket string) string {
	return "s3://" + bucket + " (" + strings.TrimSuffix(endpoint, "/") + ")"
}

// objectLabel — объект в логах: s3://bucket/key (endpoint).
func objectLabel(endpoint, bucket, key string) string {
	return "s3://" + bucket + "/" + key + " (" + strings.TrimSuffix(endpoint, "/") + ")"
}

// JobBucket — описание бакета для dump/restore-Job'а: голова кладёт список в
// секрет кред инстанса (S3_BUCKETS, JSON), jobrunner читает его оттуда.
type JobBucket struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	PathStyle       bool   `json:"path_style"`
	UseSSL          bool   `json:"use_ssl"`
}

// String — s3://bucket (endpoint) для логов Job'а.
func (b JobBucket) String() string { return bucketLabel(b.Endpoint, b.Bucket) }

// Object — s3://bucket/key (endpoint) для логов Job'а.
func (b JobBucket) Object(key string) string { return objectLabel(b.Endpoint, b.Bucket, key) }

// JobBucketOf переводит бакет в формат для Job'а.
func JobBucketOf(b model.S3Bucket) JobBucket {
	return JobBucket{
		ID: b.ID, Name: b.Name, Endpoint: b.Endpoint, Region: b.Region, Bucket: b.Bucket,
		AccessKeyID: b.AccessKeyID, SecretAccessKey: b.SecretAccessKey,
		PathStyle: b.PathStyle, UseSSL: b.UseSSL,
	}
}

// Minio создаёт minio-клиента для бакета.
func (b JobBucket) Minio() (*minio.Client, error) {
	lookup := minio.BucketLookupDNS
	if b.PathStyle {
		lookup = minio.BucketLookupPath
	}
	mc, err := minio.New(b.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(b.AccessKeyID, b.SecretAccessKey, ""),
		Secure:       b.UseSSL,
		Region:       b.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client %s: %w", b, err)
	}
	return mc, nil
}

// New создаёт S3-клиента под бакет.
func New(b model.S3Bucket) (*Client, error) {
	mc, err := JobBucketOf(b).Minio()
	if err != nil {
		return nil, err
	}
	return &Client{mc: mc, id: b.ID, title: b.Name, name: bucketLabel(b.Endpoint, b.Bucket),
		endpoint: b.Endpoint, bucket: b.Bucket, downloadable: b.Downloadable}, nil
}

// ID — id бакета (BuiltInID для бакета из env).
func (c *Client) ID() string { return c.id }

// Title — имя бакета в UI (model.S3Bucket.Name).
func (c *Client) Title() string { return c.title }

// Downloadable — можно ли выдавать из бакета presigned-ссылки на дампы.
func (c *Client) Downloadable() bool { return c.downloadable }

// Name — бакет для логов и нотификаций: s3://bucket (endpoint).
func (c *Client) Name() string { return c.name }

// Object — объект бакета для логов: s3://bucket/key (endpoint).
func (c *Client) Object(key string) string { return objectLabel(c.endpoint, c.bucket, key) }

// Ping проверяет доступность S3 и наличие бакета.
func (c *Client) Ping(ctx context.Context) error {
	ok, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("s3 %s: %w", c.name, err)
	}
	if !ok {
		return fmt.Errorf("s3 %s: bucket %q not found", c.name, c.bucket)
	}
	return nil
}

// Object — метаданные объекта в списке.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	// Ready — рядом лежит <key>.sha256, то есть дамп залит полностью.
	Ready bool
}

// ListDumps возвращает объекты дампов под префиксом (без sidecar-файлов) с
// признаком готовности — из того же листинга, без StatObject на каждый дамп.
func (c *Client) ListDumps(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	sums := map[string]bool{}
	for info := range c.mc.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if info.Err != nil {
			return nil, info.Err
		}
		if key, ok := strings.CutSuffix(info.Key, ChecksumSuffix); ok {
			sums[key] = true
			continue
		}
		if strings.HasSuffix(info.Key, SignatureSuffix) ||
			strings.HasSuffix(info.Key, ExtensionsSuffix) {
			continue
		}
		out = append(out, Object{Key: info.Key, Size: info.Size, LastModified: info.LastModified})
	}
	for i := range out {
		out[i].Ready = sums[out[i].Key]
	}
	return out, nil
}

// ChecksumReady сообщает, залит ли уже <key>.sha256 (то есть дамп готов).
func (c *Client) ChecksumReady(ctx context.Context, dumpKey string) (bool, error) {
	_, err := c.mc.StatObject(ctx, c.bucket, dumpKey+ChecksumSuffix, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return false, nil
	}
	return false, err
}

// ReadChecksum скачивает содержимое <key>.sha256 (обычно "<hex>  <filename>").
func (c *Client) ReadChecksum(ctx context.Context, dumpKey string) (string, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, dumpKey+ChecksumSuffix, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer obj.Close()
	b, err := io.ReadAll(io.LimitReader(obj, 4096))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// PutSignature кладёт подпись манифеста рядом с дампом (<key>.sig).
func (c *Client) PutSignature(ctx context.Context, dumpKey, sig string) error {
	body := []byte(sig + "\n")
	_, err := c.mc.PutObject(ctx, c.bucket, dumpKey+SignatureSuffix,
		bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: "text/plain"})
	return err
}

// ReadSignature читает <key>.sig. Отсутствие файла — ошибка: неподписанный дамп
// восстанавливать нельзя.
func (c *Client) ReadSignature(ctx context.Context, dumpKey string) (string, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, dumpKey+SignatureSuffix, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}
	defer obj.Close()
	b, err := io.ReadAll(io.LimitReader(obj, 512))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// PresignGet выдаёт временную ссылку на скачивание объекта. Непустой filename
// уходит в response-content-disposition: браузер сохранит файл под этим именем.
// Подпись считается локально (регион задан в клиенте), в S3 запроса нет.
func (c *Client) PresignGet(ctx context.Context, key, filename string, ttl time.Duration) (string, error) {
	params := url.Values{}
	if filename != "" {
		params.Set("response-content-disposition", `attachment; filename="`+filename+`"`)
	}
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, params)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

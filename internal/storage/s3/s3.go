// Package s3 предоставляет интерфейс object storage и MinIO-реализацию.
//
// Выбранный подход к загрузке медиа: multipart upload на API, который сам
// сохраняет объект в S3. Это проще для Flutter-клиента и позволяет проверять
// авторизацию/лимиты на нашей стороне. Presigned PUT также поддерживается
// (PresignPut) и может использоваться позже для прямой загрузки с клиента.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"memorybutton/backend/internal/config"
)

// Storage — абстракция object storage (аудио/фото).
type Storage interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	URL(ctx context.Context, key string) (string, error)
	PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// ---------- MinIO ----------

type Minio struct {
	client *minio.Client
	bucket string
	public string
}

func NewMinio(cfg config.S3Config) (*Minio, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("init minio client: %w", err)
	}
	m := &Minio{client: client, bucket: cfg.Bucket, public: strings.TrimRight(cfg.PublicURL, "/")}
	if err := m.ensureBucket(context.Background(), cfg.Region); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Minio) ensureBucket(ctx context.Context, region string) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := m.client.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		exists2, e2 := m.client.BucketExists(ctx, m.bucket)
		if e2 == nil && exists2 {
			return nil
		}
		return fmt.Errorf("make bucket: %w", err)
	}
	return nil
}

func (m *Minio) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, minio.PutObjectOptions{ContentType: mime})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

func (m *Minio) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (m *Minio) Delete(ctx context.Context, key string) error {
	if err := m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object %s: %w", key, err)
	}
	return nil
}

func (m *Minio) URL(ctx context.Context, key string) (string, error) {
	if m.public != "" {
		return m.public + "/" + m.bucket + "/" + pathEscape(key), nil
	}
	return m.PresignGet(ctx, key, 24*time.Hour)
}

func (m *Minio) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := m.client.PresignedPutObject(ctx, m.bucket, key, ttl)
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return u.String(), nil
}

func (m *Minio) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := m.client.PresignedGetObject(ctx, m.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return u.String(), nil
}

func pathEscape(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ---------- In-memory fake для тестов ----------

type Memory struct {
	objects map[string][]byte
}

func NewMemory() *Memory { return &Memory{objects: map[string][]byte{}} }

func (m *Memory) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.objects[key] = b
	return nil
}

func (m *Memory) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(strings.NewReader(string(b))), nil
}

func (m *Memory) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func (m *Memory) URL(_ context.Context, key string) (string, error) {
	return "memory://" + key, nil
}

func (m *Memory) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return "memory-put://" + key, nil
}

func (m *Memory) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return "memory-get://" + key, nil
}

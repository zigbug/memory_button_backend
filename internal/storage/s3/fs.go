package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FS — простое файловое хранилище, реализует Storage для локального деплоя без MinIO.
type FS struct {
	root      string
	publicURL string
}

func NewFS(root, publicURL string) (*FS, error) {
	if root == "" {
		return nil, errors.New("fs storage root is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir fs root: %w", err)
	}
	return &FS{root: root, publicURL: strings.TrimRight(publicURL, "/")}, nil
}

func (f *FS) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if strings.Contains(clean, "..") {
		return "", errors.New("invalid storage key")
	}
	return filepath.Join(f.root, filepath.FromSlash(strings.TrimPrefix(clean, "/"))), nil
}

func (f *FS) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (f *FS) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (f *FS) Delete(_ context.Context, key string) error {
	p, err := f.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (f *FS) URL(_ context.Context, key string) (string, error) {
	if f.publicURL != "" {
		return f.publicURL + "/" + strings.TrimLeft(key, "/"), nil
	}
	return "file://" + key, nil
}

func (f *FS) PresignPut(_ context.Context, key string, _ time.Duration) (string, error) {
	return f.URL(context.Background(), key)
}

func (f *FS) PresignGet(_ context.Context, key string, _ time.Duration) (string, error) {
	return f.URL(context.Background(), key)
}

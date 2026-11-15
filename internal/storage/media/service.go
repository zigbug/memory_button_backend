// Package media связывает object storage и таблицу media: загрузка, URL, выдача.
package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/google/uuid"

	"memorybutton/backend/internal/domain"
	"memorybutton/backend/internal/storage/postgres"
	"memorybutton/backend/internal/storage/s3"
)

type Service struct {
	store   *postgres.Store
	storage s3.Storage
}

func NewService(store *postgres.Store, storage s3.Storage) *Service {
	return &Service{store: store, storage: storage}
}

// Upload сохраняет байты в object storage и регистрирует запись в БД.
func (s *Service) Upload(ctx context.Context, kind domain.MediaKind, ownerKind, ownerID, filename string, data []byte, mime string) (domain.Media, error) {
	var m domain.Media
	id := uuid.NewString()
	ext := strings.ToLower(path.Ext(filename))
	if ext == "" {
		ext = extensionForMIME(mime)
	}
	key := fmt.Sprintf("%s/%s/%s/%s%s", kind, ownerKind, ownerID, id, ext)
	if mime == "" {
		mime = "application/octet-stream"
	}
	if err := s.storage.Put(ctx, key, strings.NewReader(string(data)), int64(len(data)), mime); err != nil {
		return m, err
	}
	url, err := s.storage.URL(ctx, key)
	if err != nil {
		url = ""
	}
	m = domain.Media{
		ID:         id,
		Kind:       kind,
		StorageKey: key,
		URL:        url,
		MIME:       mime,
		SizeBytes:  int64(len(data)),
		OwnerKind:  ownerKind,
	}
	if ownerID != "" {
		m.OwnerID = &ownerID
	}
	if err := s.store.CreateMedia(ctx, &m); err != nil {
		return m, err
	}
	return m, nil
}

// UploadReader — то же, что Upload, но из потока (multipart).
func (s *Service) UploadReader(ctx context.Context, kind domain.MediaKind, ownerKind, ownerID, filename string, r io.Reader, size int64, mime string) (domain.Media, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxMediaSize+1))
	if err != nil {
		return domain.Media{}, err
	}
	if int64(len(data)) > maxMediaSize {
		return domain.Media{}, ErrTooLarge
	}
	return s.Upload(ctx, kind, ownerKind, ownerID, filename, data, mime)
}

const maxMediaSize = 64 << 20

var ErrTooLarge = errors.New("медиа больше 64 МБ")

// Get возвращает медиа и поток для скачивания.
func (s *Service) Get(ctx context.Context, id string) (domain.Media, io.ReadCloser, error) {
	m, err := s.store.GetMedia(ctx, id)
	if err != nil {
		return m, nil, err
	}
	rc, err := s.storage.Get(ctx, m.StorageKey)
	if err != nil {
		return m, nil, err
	}
	return m, rc, nil
}

func extensionForMIME(mime string) string {
	switch mime {
	case "audio/webm":
		return ".webm"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/m4a":
		return ".m4a"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/heic":
		return ".heic"
	default:
		return ".bin"
	}
}

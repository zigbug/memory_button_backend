#!/bin/sh
set -e
# Фикс прав на volume для файлового хранилища (контейнер может стартовать как root).
if [ "$(id -u)" = "0" ]; then
  if [ -d "/data/storage" ]; then
    chown -R app:app /data/storage || true
  fi
  # Миграции и бинарь не требуют root.
  exec su-exec app /app/memory-button-api "$@"
fi
exec /app/memory-button-api "$@"

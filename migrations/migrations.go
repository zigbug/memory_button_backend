// Package migrations встраивает SQL-миграции в бинарник, чтобы API и worker
// применяли одну и ту же схему при старте.
package migrations

import "embed"

// FS содержит файлы *.sql в алфавитном порядке: NNNNNN_name.up.sql / .down.sql.
//
//go:embed *.sql
var FS embed.FS

package migrations

import (
	"github.com/jmoiron/sqlx"
	"github.com/knadh/koanf/v2"
	"github.com/knadh/stuffbin"
)

func V2_9_0(db *sqlx.DB, _ stuffbin.FileSystem, _ *koanf.Koanf) error {
	_, err := db.Exec(`ALTER TYPE "media_store" ADD VALUE IF NOT EXISTS 'azblob'`)
	return err
}

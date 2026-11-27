package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
)

// VacuumInto пишет согласованную копию базы в новый файл, не останавливая работу: файл path не должен существовать.
func (db *DB) VacuumInto(ctx context.Context, path string) error {
	_, err := db.W.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

// CheckFile проверяет файл базы: он открывается, проходит PRAGMA integrity_check и это база Worker
// (миграции применены). Файл открывается только на чтение и не меняется.
func CheckFile(path string) error {
	q := url.Values{}
	q.Set("mode", "ro")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("файл не открывается как база: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		if line != "ok" {
			return errors.New("база повреждена: " + line)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == 0 {
		return errors.New("это не база Worker: миграции не применены")
	}
	return nil
}

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"

	_ "modernc.org/sqlite" // драйвер SQLite на чистом Go, без CGO
)

// DB это база Worker. SQLite пишет по одному, поэтому запись идёт через одно соединение W,
// а чтение через пул R. Так не бывает ошибки «database is locked».
type DB struct {
	W *sql.DB // единственное соединение на запись
	R *sql.DB // пул только для чтения
}

const readConns = 4

// Open открывает базу, применяет миграции и готовит соединения.
func Open(path string) (*DB, error) {
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)

	if err := migrate(w, migrationsFS); err != nil {
		w.Close()
		return nil, fmt.Errorf("миграции: %w", err)
	}

	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(readConns)
	// В базе хэши паролей, шифротексты ключей и ссылки подписок: читать её может только владелец процесса.
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(f, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			w.Close()
			r.Close()
			return nil, fmt.Errorf("права на файл базы: %w", err)
		}
	}
	return &DB{W: w, R: r}, nil
}

// Close закрывает оба пула.
func (db *DB) Close() error {
	rerr := db.R.Close()
	if err := db.W.Close(); err != nil {
		return err
	}
	return rerr
}

// dsn собирает строку подключения. WAL даёт читать во время записи, busy_timeout ждёт вместо ошибки,
// foreign_keys включает проверку ссылок. Транзакции на запись сразу берут блокировку (immediate),
// чтобы две транзакции не столкнулись при переходе от чтения к записи.
func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + q.Encode()
}

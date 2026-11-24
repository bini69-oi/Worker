package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate применяет миграции из папки migrations по порядку номеров.
// Номер последней применённой хранится в PRAGMA user_version, поэтому каждая применяется один раз.
// Каждая миграция идёт в своей транзакции вместе со сменой номера.
func migrate(db *sql.DB, fsys fs.FS) error {
	files, err := migrationFiles(fsys)
	if err != nil {
		return err
	}

	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}

	for _, f := range files {
		if f.num <= current {
			continue
		}
		body, err := fs.ReadFile(fsys, f.path)
		if err != nil {
			return err
		}
		if err := applyMigration(db, f.num, string(body)); err != nil {
			return fmt.Errorf("%s: %w", f.path, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, num int, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(body); err != nil {
		return err
	}
	// PRAGMA не принимает параметры, поэтому число подставляем само. Это наше число, не ввод пользователя.
	if _, err := tx.Exec("PRAGMA user_version = " + strconv.Itoa(num)); err != nil {
		return err
	}
	return tx.Commit()
}

type migrationFile struct {
	num  int
	path string
}

// migrationFiles находит файлы вида 001_name.sql и сортирует их по номеру. Повтор номера это ошибка.
func migrationFiles(fsys fs.FS) ([]migrationFile, error) {
	paths, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var files []migrationFile
	seen := map[int]string{}
	for _, p := range paths {
		prefix, _, _ := strings.Cut(path.Base(p), "_")
		num, err := strconv.Atoi(prefix)
		if err != nil || num <= 0 {
			return nil, fmt.Errorf("%s: имя должно начинаться с номера, например 001_init.sql", p)
		}
		if other, ok := seen[num]; ok {
			return nil, fmt.Errorf("номер %d повторяется: %s и %s", num, other, p)
		}
		seen[num] = p
		files = append(files, migrationFile{num: num, path: p})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].num < files[j].num })
	return files, nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/bini69-oi/worker/core"
)

// CoreStore реализует core.Store на SQLite.
type CoreStore struct{ db *DB }

// NewCoreStore возвращает хранилище ядра поверх базы.
func NewCoreStore(db *DB) *CoreStore { return &CoreStore{db: db} }

var _ core.Store = (*CoreStore)(nil)

// Tx открывает транзакцию на соединении записи. Ошибка из fn откатывает всё.
func (s *CoreStore) Tx(ctx context.Context, fn func(core.Tx) error) error {
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&coreTx{ctx: ctx, q: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *CoreStore) User(ctx context.Context, id int64) (core.User, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).User(id)
}

func (s *CoreStore) UserByTg(ctx context.Context, tgID int64) (core.User, error) {
	return (&coreTx{ctx: ctx, q: s.db.R}).UserByTg(tgID)
}

func (s *CoreStore) Ledger(ctx context.Context, userID int64, limit int) ([]core.LedgerEntry, error) {
	rows, err := s.db.R.QueryContext(ctx,
		`SELECT id, user_id, amount, reason, ref_type, ref_id, admin_id, comment, created_at
		 FROM ledger WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.LedgerEntry
	for rows.Next() {
		var e core.LedgerEntry
		var reason string
		var refID, adminID sql.NullInt64
		var created int64
		if err := rows.Scan(&e.ID, &e.UserID, &e.Amount, &reason, &e.RefType, &refID, &adminID, &e.Comment, &created); err != nil {
			return nil, err
		}
		e.Reason = core.Reason(reason)
		e.RefID, e.AdminID = refID.Int64, adminID.Int64
		e.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// querier это то общее, что есть у *sql.DB и *sql.Tx.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type coreTx struct {
	ctx context.Context
	q   querier
}

const userCols = `id, tg_id, username, lang, referrer_id, ref_code, balance, trial_used, banned_at, created_at, unreachable_at, screen_msg_id`

func (t *coreTx) User(id int64) (core.User, error) {
	return scanUser(t.q.QueryRowContext(t.ctx, "SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func (t *coreTx) UserByTg(tgID int64) (core.User, error) {
	return scanUser(t.q.QueryRowContext(t.ctx, "SELECT "+userCols+" FROM users WHERE tg_id = ?", tgID))
}

func (t *coreTx) UserByRef(code string) (core.User, error) {
	return scanUser(t.q.QueryRowContext(t.ctx, "SELECT "+userCols+" FROM users WHERE ref_code = ? AND ref_code <> ''", code))
}

func scanUser(row *sql.Row) (core.User, error) {
	var u core.User
	var referrer, banned, unreachable sql.NullInt64
	var created int64
	err := row.Scan(&u.ID, &u.TgID, &u.Username, &u.Lang, &referrer, &u.RefCode, &u.Balance, &u.TrialUsed, &banned, &created,
		&unreachable, &u.ScreenMsgID)
	if errors.Is(err, sql.ErrNoRows) {
		return u, core.ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.ReferrerID = referrer.Int64
	if banned.Valid {
		u.BannedAt = time.Unix(banned.Int64, 0).UTC()
	}
	if unreachable.Valid {
		u.UnreachableAt = time.Unix(unreachable.Int64, 0).UTC()
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	return u, nil
}

func (t *coreTx) CreateUser(u core.User) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		`INSERT INTO users (tg_id, username, lang, referrer_id, ref_code, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.TgID, u.Username, u.Lang, nullID(u.ReferrerID), u.RefCode, u.CreatedAt.Unix())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

// AddLedger пишет строку истории и меняет кэш баланса. Обе записи идут в одной транзакции.
func (t *coreTx) AddLedger(e core.LedgerEntry) (int64, error) {
	res, err := t.q.ExecContext(t.ctx,
		`INSERT INTO ledger (user_id, amount, reason, ref_type, ref_id, admin_id, comment, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.Amount, string(e.Reason), e.RefType, nullID(e.RefID), nullID(e.AdminID), e.Comment, e.CreatedAt.Unix())
	if err != nil {
		return 0, mapErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	upd, err := t.q.ExecContext(t.ctx, "UPDATE users SET balance = balance + ? WHERE id = ?", e.Amount, e.UserID)
	if err != nil {
		// Единственная проверка на users.balance это «не меньше нуля».
		if errors.Is(mapErr(err), core.ErrInvalid) {
			return 0, core.ErrNotEnoughMoney
		}
		return 0, mapErr(err)
	}
	if n, _ := upd.RowsAffected(); n != 1 {
		return 0, core.ErrNotFound
	}
	return id, nil
}

// nullID превращает 0 в NULL: так в базе видно, что ссылки нет.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// mapErr переводит ошибки ограничений SQLite в ошибки ядра.
func mapErr(err error) error {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return err
	}
	switch se.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return core.ErrDuplicate
	case sqlite3.SQLITE_CONSTRAINT_CHECK:
		return core.ErrInvalid
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return core.ErrNotFound
	}
	return err
}

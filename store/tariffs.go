package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/bini69-oi/worker/core"
)

const tariffCols = `id, name, traffic_bytes, devices, reset, squads, active, sort, created_at`

func (s *CoreStore) Tariffs(ctx context.Context, onlyActive bool) ([]core.Tariff, error) {
	q := "SELECT " + tariffCols + " FROM tariffs"
	if onlyActive {
		q += " WHERE active = 1"
	}
	q += " ORDER BY sort, id"
	rows, err := s.db.R.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	var list []core.Tariff
	for rows.Next() {
		t, err := scanTariff(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Periods, err = loadPeriods(ctx, s.db.R, list[i].ID); err != nil {
			return nil, err
		}
	}
	return list, nil
}

func (t *coreTx) Tariff(id int64) (core.Tariff, error) {
	tr, err := scanTariff(t.q.QueryRowContext(t.ctx, "SELECT "+tariffCols+" FROM tariffs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return tr, core.ErrNotFound
	}
	if err != nil {
		return tr, err
	}
	tr.Periods, err = loadPeriods(t.ctx, t.q, id)
	return tr, err
}

// SaveTariff создаёт или заменяет тариф. Периоды заменяются целиком.
func (t *coreTx) SaveTariff(tr core.Tariff) (int64, error) {
	squads, err := json.Marshal(nonNil(tr.Squads))
	if err != nil {
		return 0, err
	}
	if tr.ID == 0 {
		res, err := t.q.ExecContext(t.ctx,
			`INSERT INTO tariffs (name, traffic_bytes, devices, reset, squads, active, sort, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tr.Name, tr.TrafficBytes, tr.Devices, tr.Reset, string(squads), tr.Active, tr.Sort, tr.CreatedAt.Unix())
		if err != nil {
			return 0, mapErr(err)
		}
		if tr.ID, err = res.LastInsertId(); err != nil {
			return 0, err
		}
	} else {
		res, err := t.q.ExecContext(t.ctx,
			`UPDATE tariffs SET name = ?, traffic_bytes = ?, devices = ?, reset = ?, squads = ?, active = ?, sort = ?
			 WHERE id = ?`,
			tr.Name, tr.TrafficBytes, tr.Devices, tr.Reset, string(squads), tr.Active, tr.Sort, tr.ID)
		if err != nil {
			return 0, mapErr(err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, core.ErrNotFound
		}
		if _, err := t.q.ExecContext(t.ctx, "DELETE FROM tariff_periods WHERE tariff_id = ?", tr.ID); err != nil {
			return 0, err
		}
	}
	for _, p := range tr.Periods {
		if _, err := t.q.ExecContext(t.ctx,
			"INSERT INTO tariff_periods (tariff_id, days, price) VALUES (?, ?, ?)", tr.ID, p.Days, p.Price); err != nil {
			return 0, mapErr(err)
		}
	}
	return tr.ID, nil
}

// scanner это то общее, что есть у *sql.Row и *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTariff(r scanner) (core.Tariff, error) {
	var t core.Tariff
	var squads string
	var created int64
	if err := r.Scan(&t.ID, &t.Name, &t.TrafficBytes, &t.Devices, &t.Reset, &squads, &t.Active, &t.Sort, &created); err != nil {
		return t, err
	}
	if err := json.Unmarshal([]byte(squads), &t.Squads); err != nil {
		return t, err
	}
	t.CreatedAt = time.Unix(created, 0).UTC()
	return t, nil
}

func loadPeriods(ctx context.Context, q querier, tariffID int64) ([]core.Period, error) {
	rows, err := q.QueryContext(ctx, "SELECT days, price FROM tariff_periods WHERE tariff_id = ? ORDER BY days", tariffID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Period
	for rows.Next() {
		var p core.Period
		if err := rows.Scan(&p.Days, &p.Price); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

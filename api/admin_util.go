package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bini69-oi/worker/core"
)

// pathID читает числовой {id} из адреса. Не число: ErrInvalid.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, core.ErrInvalid
	}
	return id, nil
}

// queryCursor читает курсор страницы из ?cursor=. Нет курсора: 0, первая страница.
func queryCursor(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, core.ErrInvalid
	}
	return n, nil
}

// optTime это время для ответа: нулевое время отдаётся как null.
func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

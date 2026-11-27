package api

import (
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminPaymentList это страница платежей.
type AdminPaymentList struct {
	Items []AdminPayment `json:"items"`
	Next  int64          `json:"next"`
}

// parseDay читает дату из запроса: 2026-01-31 или полное время RFC 3339. Пусто: нулевое время.
func parseDay(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, core.ErrInvalid
}

func (a *adminAPI) paymentsList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	cur, err := queryCursor(r)
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	f := core.PaymentFilter{Status: core.PayStatus(q.Get("status")), Gateway: q.Get("gateway")}
	if f.From, err = parseDay(q.Get("from")); err != nil {
		return nil, err
	}
	if f.To, err = parseDay(q.Get("to")); err != nil {
		return nil, err
	}
	if !f.To.IsZero() && len(q.Get("to")) == 10 {
		f.To = f.To.AddDate(0, 0, 1) // «по дату» включает весь этот день
	}
	page, err := a.d.Core.Payments(r.Context(), ad.Admin.TgID, f, cur)
	if err != nil {
		return nil, err
	}
	out := AdminPaymentList{Items: make([]AdminPayment, 0, len(page.Items)), Next: page.Next}
	for _, row := range page.Items {
		out.Items = append(out.Items, paymentDTO(row.Payment, row.Username))
	}
	return out, nil
}

func (a *adminAPI) paymentConfirm(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return nil, a.d.Core.ConfirmPayment(r.Context(), ad.Admin.TgID, id)
}

func (a *adminAPI) paymentRefund(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return nil, a.d.Core.RefundStars(r.Context(), ad.Admin.TgID, id)
}

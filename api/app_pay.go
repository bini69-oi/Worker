package api

import (
	"net/http"
	"strconv"

	"github.com/bini69-oi/worker/core"
)

// maxTopup это предел суммы пополнения в копейках: защита от опечатки. Тот же, что в боте.
const maxTopup = 1_000_000 * 100

func (a *App) payRoutes() {
	a.handle("POST /api/app/topup", a.topup)
	// Статус платежа виден и во время техработ: оплата зачисляется всегда, абонент ждёт её результата.
	a.handleOpen("GET /api/app/payments/{id}", a.payment)
}

// topup создаёт платёж. Stars: ссылка на счёт Telegram, её открывает WebApp.openInvoice. Остальные кассы:
// ссылка на оплату или реквизиты. Оплату зачислит вебхук кассы или сверка при опросе статуса.
func (a *App) topup(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in TopupRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if in.Amount <= 0 || in.Amount > maxTopup {
		return nil, core.ErrInvalid
	}
	if in.Gateway == "stars" && a.StarsInvoice == nil {
		return nil, core.ErrNoGateway
	}
	p, inv, err := a.Core.CreatePayment(r.Context(), au.User.ID, in.Gateway, in.Amount)
	if err != nil {
		return nil, err
	}
	out := TopupResponse{PaymentID: p.ID, Gateway: p.Gateway, URL: inv.URL, Details: inv.Text,
		ExtAmount: inv.ExtAmount, ExtCurrency: inv.ExtCurrency}
	if inv.ExtCurrency == "XTR" {
		title := a.srv.deps.Text("app.stars_title", au.User.Lang)
		desc := a.srv.deps.Text("app.stars_desc", au.User.Lang)
		out.InvoiceLink, err = a.StarsInvoice(r.Context(), title, desc, "p:"+strconv.FormatInt(p.ID, 10), inv.ExtAmount)
		if err != nil {
			a.srv.deps.Log.Warn("счёт Stars не создан", "payment", p.ID, "err", err)
			return nil, core.ErrGatewayDown
		}
	}
	return out, nil
}

// payment отдаёт статус платежа абонента. Ядро само сверяет ожидающий платёж с кассой не чаще раза в 3 секунды.
// Чужой и несуществующий платёж отвечают одинаково: 404.
func (a *App) payment(_ http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return nil, core.ErrNotFound
	}
	p, err := a.Core.MyPayment(r.Context(), au.User.ID, id)
	if err != nil {
		return nil, err
	}
	out := PaymentResponse{ID: p.ID, Gateway: p.Gateway, Amount: p.Amount, Status: string(p.Status), CreatedAt: p.CreatedAt}
	if !p.PaidAt.IsZero() {
		out.PaidAt = &p.PaidAt
	}
	return out, nil
}

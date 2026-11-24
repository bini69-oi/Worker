package panel

import (
	"context"
	"net/http"
	"net/url"
)

// Sync приводит абонента в панели к состоянию s. Находит абонента по
// сохранённому PanelID, иначе по имени. Если его нет, создаёт, если есть,
// обновляет. Повторный вызов с тем же состоянием ничего не меняет по сути.
func (c *Client) Sync(ctx context.Context, s State) (Result, error) {
	if err := s.validate(); err != nil {
		return Result{}, err
	}
	ct, err := c.current(ctx)
	if err != nil {
		return Result{}, err
	}
	existing, found, err := c.find(ctx, ct, s)
	if err != nil {
		return Result{}, err
	}
	body := payload(s, ct, c.now())
	var u wireUser
	if !found {
		if err := c.do(ctx, http.MethodPost, "/api/users", body, &u); err != nil {
			return Result{}, err
		}
		return ct.result(u, true), nil
	}
	key, id := ct.identity(ct.refOf(existing))
	body[key] = id
	if err := c.do(ctx, http.MethodPatch, "/api/users", body, &u); err != nil {
		return Result{}, err
	}
	return ct.result(u, false), nil
}

// find ищет абонента: сначала по сохранённому идентификатору, потом по имени.
func (c *Client) find(ctx context.Context, ct contract, s State) (wireUser, bool, error) {
	if ct.validRef(s.PanelID) {
		u, err := c.byRef(ctx, s.PanelID)
		if err == nil {
			return u, true, nil
		}
		if !IsNotFound(err) {
			return wireUser{}, false, err
		}
	}
	u, err := c.byUsername(ctx, s.Username)
	if err == nil {
		return u, true, nil
	}
	if IsNotFound(err) {
		return wireUser{}, false, nil
	}
	return wireUser{}, false, err
}

// userPath это путь абонента для действий над ним.
// Get читает абонента из панели по сохранённому идентификатору. Нужен для сверки с базой.
func (c *Client) Get(ctx context.Context, ref string) (Result, error) {
	ct, err := c.current(ctx)
	if err != nil {
		return Result{}, err
	}
	if !ct.validRef(ref) {
		return Result{}, &Error{Status: 404, Msg: "абонент не найден"}
	}
	u, err := c.byRef(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	return ct.result(u, false), nil
}

func userPath(ref string) string { return "/api/users/" + url.PathEscape(ref) }

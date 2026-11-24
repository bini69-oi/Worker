package panel

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Здесь собраны все отличия Remnawave 2.x от 3.x. Остальной код о версии не знает.
//
//	2.x: абонент адресуется по uuid, обновление PATCH /api/users с "uuid" в теле,
//	     устройства удаляются по "userUuid".
//	3.x: абонент адресуется по числовому id, обновление PATCH /api/users с "id" в теле,
//	     устройства удаляются по "userId", срок в прошлом панель отклоняет.
//
// Сверено с контрактом панели (libs/contract) версий 2.8.1 и 3.4.17.
type contract struct {
	v3 bool
}

func contractFor(v Version) contract { return contract{v3: v.Generation() >= 3} }

// refOf возвращает идентификатор абонента, которым его зовут в этой версии.
func (c contract) refOf(u wireUser) string {
	if c.v3 {
		if u.ID > 0 {
			return strconv.FormatInt(int64(u.ID), 10)
		}
		return ""
	}
	return u.UUID
}

// validRef проверяет, что сохранённый идентификатор подходит этой версии панели.
// Если панель обновили с 2.x до 3.x, старый uuid не годится и абонента ищут по имени.
func (c contract) validRef(ref string) bool {
	if ref == "" {
		return false
	}
	if c.v3 {
		n, err := strconv.ParseInt(ref, 10, 64)
		return err == nil && n > 0
	}
	return true
}

// identity возвращает пару «поле, значение» для тела запроса на изменение.
func (c contract) identity(ref string) (string, any) {
	if c.v3 {
		n, _ := strconv.ParseInt(ref, 10, 64)
		return "id", n
	}
	return "uuid", ref
}

// deviceBody это тело запроса к устройствам абонента. Пустой hwid значит «все устройства».
func (c contract) deviceBody(ref, hwid string) map[string]any {
	body := map[string]any{"userUuid": ref}
	if c.v3 {
		n, _ := strconv.ParseInt(ref, 10, 64)
		body = map[string]any{"userId": n}
	}
	if hwid != "" {
		body["hwid"] = hwid
	}
	return body
}

// expireText переводит срок в текст для панели. Панель 3.x не принимает срок
// в прошлом, поэтому такой срок сдвигается на две минуты вперёд.
func (c contract) expireText(t, now time.Time) string {
	t = t.UTC().Truncate(time.Millisecond)
	if c.v3 && !t.After(now) {
		t = now.Add(2 * time.Minute).UTC().Truncate(time.Millisecond)
	}
	return t.Format("2006-01-02T15:04:05.000Z")
}

// byUsername ищет абонента по имени, путь одинаковый в 2.x и 3.x. Не нашли: ошибка с IsNotFound.
func (cl *Client) byUsername(ctx context.Context, username string) (wireUser, error) {
	var u wireUser
	err := cl.do(ctx, http.MethodGet, "/api/users/by-username/"+url.PathEscape(username), nil, &u)
	return u, err
}

// byRef берёт абонента по сохранённому идентификатору.
func (cl *Client) byRef(ctx context.Context, ref string) (wireUser, error) {
	var u wireUser
	err := cl.do(ctx, http.MethodGet, "/api/users/"+url.PathEscape(ref), nil, &u)
	return u, err
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bini69-oi/worker/settings"
)

// recordingTelegram отвечает «ок» на любой метод Bot API и запоминает отправленные сообщения.
type recordingTelegram struct {
	mu   sync.Mutex
	sent []map[string]any
}

func (r *recordingTelegram) handler(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	switch {
	case strings.HasSuffix(req.URL.Path, "/getMe"):
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"username":"workerbot"}}`))
		return
	case strings.HasSuffix(req.URL.Path, "/getUpdates"):
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		return
	case strings.HasSuffix(req.URL.Path, "/sendMessage"):
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			r.mu.Lock()
			r.sent = append(r.sent, m)
			r.mu.Unlock()
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":1}}}`))
		return
	}
	_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
}

func (r *recordingTelegram) messagesTo(chat int64) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, m := range r.sent {
		if c, ok := m["chat_id"].(float64); ok && int64(c) == chat {
			out = append(out, m)
		}
	}
	return out
}

// adminClient ходит в API админки настоящего процесса: cookie ведёт сам, как это делает страница.
type adminClient struct {
	t      *testing.T
	base   string
	cookie string
}

func (c *adminClient) do(method, path, body string) (int, string, http.Header) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if method != http.MethodGet {
		req.Header.Set("X-Worker", "1")
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw), resp.Header
}

func (c *adminClient) json(method, path, body string, out any) {
	c.t.Helper()
	code, raw, _ := c.do(method, path, body)
	if code != 200 {
		c.t.Fatalf("%s %s: %d %s", method, path, code, raw)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(raw), out); err != nil {
			c.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
}

// Сквозной сценарий админки на настоящем процессе: вход паролем и кодом, поиск абонента, +30 дней, рассылка себе,
// смена реквизитов ручной оплаты с «Да» владельца.
func TestAdminEndToEnd(t *testing.T) {
	ownerPass := "correct horse battery"
	rec := &recordingTelegram{}
	tgSrv := httptest.NewServer(http.HandlerFunc(rec.handler))
	t.Cleanup(tgSrv.Close)
	env := Env{BotToken: "123456:secret-bot-token", OwnerID: 1, PublicURL: "https://bot.example.com",
		PanelURL: "http://127.0.0.1:1", PanelToken: "secret-panel-token", SecretKey: settings.NewKey(),
		DBPath: filepath.Join(t.TempDir(), "w.db"), TelegramURL: tgSrv.URL}
	a, err := build(env, slog.NewJSONHandler(io.Discard, nil))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.handler)
	t.Cleanup(func() { srv.Close(); a.db.Close() })
	ctx := t.Context()

	if err := a.core.SetOwnerPassword(ctx, ownerPass); err != nil {
		t.Fatal(err)
	}
	u, err := a.core.Register(ctx, 2001, "ivan", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.core.Trial(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	before, err := a.core.Sub(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	c := &adminClient{t: t, base: srv.URL}

	// 1. Вход: пароль, код на экране, нажатие кнопки с этим кодом в Telegram, опрос.
	if code, _, _ := c.do("GET", "/api/admin/me", ""); code != 401 {
		t.Fatalf("без входа: %d", code)
	}
	var started struct{ ID, Code string }
	c.json("POST", "/api/admin/login", fmt.Sprintf(`{"tg_id":1,"password":%q}`, ownerPass), &started)
	var confirmID int64
	if _, err := fmt.Sscanf(started.ID, "%d.", &confirmID); err != nil {
		t.Fatal(err)
	}
	if err := a.core.AnswerLogin(ctx, confirmID, 1, started.Code); err != nil {
		t.Fatal(err)
	}
	code, raw, hdr := c.do("GET", "/api/admin/login/"+started.ID, "")
	if code != 200 || !strings.Contains(raw, `"ok"`) {
		t.Fatalf("опрос входа: %d %s", code, raw)
	}
	c.cookie = strings.SplitN(hdr.Get("Set-Cookie"), ";", 2)[0]
	if !strings.HasPrefix(c.cookie, "worker_admin=") {
		t.Fatalf("cookie админки не поставлена: %q", hdr.Get("Set-Cookie"))
	}
	var me struct {
		TgID  int64 `json:"tg_id"`
		Owner bool  `json:"owner"`
	}
	c.json("GET", "/api/admin/me", "", &me)
	if me.TgID != 1 || !me.Owner {
		t.Fatalf("me: %+v", me)
	}

	// 2. Поиск и карточка.
	var list struct {
		Items []struct {
			ID       int64
			Username string
		}
	}
	c.json("GET", "/api/admin/users?q=2001", "", &list)
	if len(list.Items) != 1 || list.Items[0].Username != "ivan" || list.Items[0].ID != u.ID {
		t.Fatalf("поиск: %+v", list)
	}

	// 3. +30 дней.
	c.json("POST", fmt.Sprintf("/api/admin/users/%d/days", u.ID), `{"days":30}`, nil)
	after, err := a.core.Sub(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.ExpiresAt.Sub(before.ExpiresAt); got != 30*24*time.Hour {
		t.Fatalf("срок сдвинулся на %v", got)
	}

	// 4. Рассылка себе: сообщение пришло только владельцу, счётчики рассылки не изменились.
	var bc struct{ ID int64 }
	c.json("POST", "/api/admin/broadcasts", `{"segment":"all","text":"<b>Проверка</b>"}`, &bc)
	c.json("POST", fmt.Sprintf("/api/admin/broadcasts/%d/test", bc.ID), `{}`, nil)
	got := rec.messagesTo(1)
	if len(got) != 1 || got[0]["text"] != "<b>Проверка</b>" || got[0]["parse_mode"] != "HTML" {
		t.Fatalf("тест себе: %+v", got)
	}
	if n := len(rec.messagesTo(2001)); n != 0 {
		t.Fatalf("абонент получил тест: %d", n)
	}
	var state struct {
		Status string
		Sent   int
	}
	c.json("GET", fmt.Sprintf("/api/admin/broadcasts/%d", bc.ID), "", &state)
	if state.Status != "draft" || state.Sent != 0 {
		t.Fatalf("тест изменил рассылку: %+v", state)
	}

	// 5. Смена реквизитов: до «Да» старые, после «Да» новые, в журнале запись.
	old := a.cfg.String("manual.details")
	var res struct {
		Pending   bool
		ConfirmID int64 `json:"confirm_id"`
	}
	c.json("POST", "/api/admin/settings/set", `{"key":"manual.details","value":"Карта 4276 0000 1111 2222"}`, &res)
	if !res.Pending || res.ConfirmID == 0 {
		t.Fatalf("ждали запрос на «Да»: %+v", res)
	}
	if a.cfg.String("manual.details") != old {
		t.Fatal("реквизиты сменились до «Да»")
	}
	if err := a.core.AnswerConfirm(ctx, res.ConfirmID, 1, true); err != nil {
		t.Fatal(err)
	}
	if a.cfg.String("manual.details") != "Карта 4276 0000 1111 2222" {
		t.Fatalf("после «Да»: %q", a.cfg.String("manual.details"))
	}
	var audit struct {
		Items []struct{ Action, Target string }
	}
	c.json("GET", "/api/admin/audit?action=setting.set", "", &audit)
	if len(audit.Items) != 1 || audit.Items[0].Target != "manual.details" {
		t.Fatalf("журнал: %+v", audit)
	}

	// 6a. Список адресов: чужой адрес получает 404 и на страницу, и на API, до входа и сессии. Свой (127.0.0.1) проходит.
	if err := a.cfg.Set("admin.allow_ips", "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/admin/me", "/admin/", "/api/admin/login/1.x"} {
		if code, _, _ := c.do("GET", path, ""); code != 404 {
			t.Fatalf("%s с чужого адреса: %d", path, code)
		}
	}
	if code, _, _ := c.do("POST", "/api/admin/login", `{"tg_id":1,"password":"x"}`); code != 404 {
		t.Fatalf("вход с чужого адреса: %d", code)
	}
	if err := a.cfg.Set("admin.allow_ips", "127.0.0.1, 10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := c.do("GET", "/api/admin/me", ""); code != 200 {
		t.Fatalf("свой адрес: %d", code)
	}
	if err := a.cfg.Reset("admin.allow_ips"); err != nil {
		t.Fatal(err)
	}

	// 6. Чужая форма с cookie админа без X-Worker не меняет ничего.
	req, _ := http.NewRequest("POST", srv.URL+fmt.Sprintf("/api/admin/users/%d/days", u.ID), strings.NewReader(`days=30`))
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("чужая форма: %d", resp.StatusCode)
	}
	again, _ := a.core.Sub(ctx, u.ID)
	if !again.ExpiresAt.Equal(after.ExpiresAt) {
		t.Fatal("чужая форма сдвинула срок")
	}
}

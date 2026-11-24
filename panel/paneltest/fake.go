// Package paneltest это фейковая панель Remnawave в памяти для тестов.
// Она умеет вести себя как 2.x и как 3.x, «лежать» и считать вызовы.
package paneltest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Generation это поколение панели: 2 или 3.
type Generation int

// Поколения панели.
const (
	V2 Generation = 2
	V3 Generation = 3
)

// Token это токен, который принимает фейковая панель.
const Token = "test-token-secret"

// User это абонент в фейковой панели.
type User struct {
	UUID        string
	ID          int64
	Username    string
	Status      string
	ExpireAt    time.Time
	ShortUUID   string
	TelegramID  int64
	TrafficMax  int64
	Strategy    string
	DeviceLimit int64
	Squads      []string
	Description string
	Devices     []string
}

// Panel это фейковая панель. Все методы безопасны для одновременного использования.
type Panel struct {
	srv *httptest.Server
	gen Generation

	mu     sync.Mutex
	users  []*User
	nextID int64
	force  int
	calls  map[string]int
	now    func() time.Time
}

// New запускает фейковую панель и останавливает её по окончании теста.
func New(t testing.TB, gen Generation) *Panel {
	t.Helper()
	p := &Panel{gen: gen, nextID: 100, calls: map[string]int{}, now: time.Now}
	p.srv = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}

// URL это адрес панели.
func (p *Panel) URL() string { return p.srv.URL }

// Close останавливает панель раньше времени, чтобы проверить недоступность.
func (p *Panel) Close() { p.srv.Close() }

// SetDown включает и выключает «падение»: пока включено, панель отвечает 503.
func (p *Panel) SetDown(down bool) {
	if down {
		p.Force(http.StatusServiceUnavailable)
		return
	}
	p.Force(0)
}

// Force заставляет панель отвечать на всё указанным кодом ошибки. Ноль отключает.
func (p *Panel) Force(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.force = status
}

// Calls говорит, сколько раз вызвали маршрут, например "POST /api/users".
func (p *Panel) Calls(route string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[route]
}

// Users возвращает копии всех абонентов.
func (p *Panel) Users() []User {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]User, 0, len(p.users))
	for _, u := range p.users {
		out = append(out, *u)
	}
	return out
}

// Edit меняет абонента «руками», как это сделал бы админ в панели.
// Возвращает false, если такого имени нет.
func (p *Panel) Edit(username string, change func(*User)) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.Username == username {
			change(u)
			return true
		}
	}
	return false
}

// AddDevice привязывает устройство к абоненту.
func (p *Panel) AddDevice(username, hwid string) bool {
	return p.Edit(username, func(u *User) { u.Devices = append(u.Devices, hwid) })
}

// Ref возвращает идентификатор абонента в этой версии панели: uuid или число.
func (p *Panel) Ref(username string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.Username == username {
			return p.refOf(u)
		}
	}
	return ""
}

func (p *Panel) refOf(u *User) string {
	if p.gen == V3 {
		return strconv.FormatInt(u.ID, 10)
	}
	return u.UUID
}

func (p *Panel) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	route, args := match(r)
	p.calls[route]++
	if p.force != 0 {
		fail(w, p.force, "forced failure")
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+Token {
		fail(w, http.StatusUnauthorized, "bad token")
		return
	}
	p.route(w, r, route, args)
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func newShort() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"response": v})
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"message": msg, "statusCode": code})
}

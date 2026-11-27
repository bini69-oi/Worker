// Package guard это защита входа HTTP: настоящий IP клиента за своим прокси и ограничение частоты запросов.
package guard

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// RealIP подменяет r.RemoteAddr на адрес клиента из заголовка прокси. Заголовку верим, только если запрос
// пришёл с локального или частного адреса, то есть от своего прокси (nginx, Caddy) на том же сервере
// или в своей сети. Запрос напрямую из интернета подделать адрес заголовком не может.
func RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := fromProxy(r); ip != "" {
			r.RemoteAddr = net.JoinHostPort(ip, "0")
		}
		next.ServeHTTP(w, r)
	})
}

func fromProxy(r *http.Request) string {
	if !trusted(Host(r.RemoteAddr)) {
		return ""
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(ip) != nil {
		return ip
	}
	// В X-Forwarded-For верен только последний адрес: его дописал наш прокси, остальное прислал клиент.
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

func trusted(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// Host отдаёт IP без порта.
func Host(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// Key отдаёт ключ счётчика для адреса клиента: IPv4 как есть, IPv6 свёрнут до сети /64. У любого сервера целая /64,
// и счётчик по полному адресу обходился бы перебором адресов из неё.
func Key(addr string) string {
	host := Host(addr)
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// Limiter ограничивает число запросов с одного IP за окно. Лишние получают 429.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time

	mu   sync.Mutex
	hits map[string]*window
}

type window struct {
	start time.Time
	n     int
}

// maxTracked ограничивает память счётчиков. При переполнении сначала уходят просроченные окна, потом самые старые:
// новый ключ не получает отказ только из-за того, что память занята чужими ключами.
const maxTracked = 10000

// evict освобождает место: просроченные окна, а если их мало, десятую часть самых старых.
func (l *Limiter) evict(now time.Time) {
	for k, w := range l.hits {
		if now.Sub(w.start) >= l.Window {
			delete(l.hits, k)
		}
	}
	if len(l.hits) < maxTracked {
		return
	}
	type kv struct {
		k string
		t time.Time
	}
	all := make([]kv, 0, len(l.hits))
	for k, w := range l.hits {
		all = append(all, kv{k, w.start})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
	for _, e := range all[:maxTracked/10] {
		delete(l.hits, e.k)
	}
}

func (l *Limiter) clock() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// Over говорит, что ключ уже исчерпал предел в текущем окне. Ничего не засчитывает.
func (l *Limiter) Over(key string) bool {
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	return ok && now.Sub(w.start) < l.Window && w.n >= l.Max
}

// Allow засчитывает запрос с ключом key (IP, сессия) и говорит, пропустить ли его.
func (l *Limiter) Allow(key string) bool {
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string]*window{}
	}
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.Window {
		if !ok && len(l.hits) >= maxTracked {
			l.evict(now)
		}
		l.hits[key] = &window{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.Max
}

// Wrap пропускает запросы через ограничитель.
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(Key(r.RemoteAddr)) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

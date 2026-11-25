// Package guard это защита входа HTTP: настоящий IP клиента за своим прокси и ограничение частоты запросов.
package guard

import (
	"net"
	"net/http"
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

// maxTracked ограничивает память счётчиков: при переполнении старые окна выбрасываются.
const maxTracked = 10000

// Allow засчитывает запрос с ip и говорит, пропустить ли его.
func (l *Limiter) Allow(ip string) bool {
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string]*window{}
	}
	if len(l.hits) >= maxTracked {
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.Window {
				delete(l.hits, k)
			}
		}
	}
	w, ok := l.hits[ip]
	if !ok || now.Sub(w.start) >= l.Window {
		if len(l.hits) >= maxTracked && !ok {
			return false // память забита живыми окнами: это поток с многих адресов, новых не пускаем
		}
		l.hits[ip] = &window{start: now, n: 1}
		return true
	}
	w.n++
	return w.n <= l.Max
}

// Wrap пропускает запросы через ограничитель.
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(Host(r.RemoteAddr)) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Package redact вычищает секреты из журнала и текстов ошибок. Секрет чаще утекает через журнал
// или скриншот ошибки, чем через взлом, поэтому каждая строка журнала проходит через этот фильтр.
package redact

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// Mask это то, чем заменяется секрет.
const Mask = "***"

// minLen отсекает короткие значения: заменять «1» или «ru» в журнале бессмысленно и вредно.
const minLen = 6

// Set это набор известных секретов. Секреты добавляются при старте и при смене ключей.
type Set struct {
	mu   sync.RWMutex
	vals []string
}

// Add добавляет секреты. Пустые и короткие значения пропускаются. Вместе с секретом запоминаются его виды
// в адресе (URL-кодирование), в JSON, в base64 и hex, чтобы секрет не проскочил в журнал внутри ссылки,
// ответа или заголовка Basic. Длинные значения заменяются первыми: если один секрет входит в другой,
// длинный не откроется по частям.
func (s *Set) Add(vals ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range vals {
		if len(v) < minLen {
			continue
		}
		b := []byte(v)
		forms := []string{v, url.QueryEscape(v), url.PathEscape(v), strings.ReplaceAll(v, "/", `\/`),
			base64.StdEncoding.EncodeToString(b), base64.RawStdEncoding.EncodeToString(b),
			base64.URLEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b), hex.EncodeToString(b)}
		if j, err := json.Marshal(v); err == nil {
			forms = append(forms, strings.Trim(string(j), `"`))
		}
		for _, f := range forms {
			if !slices.Contains(s.vals, f) {
				s.vals = append(s.vals, f)
			}
		}
	}
	slices.SortStableFunc(s.vals, func(a, b string) int { return len(b) - len(a) })
}

// String заменяет в строке все известные секреты на ***.
func (s *Set) String(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.vals {
		if strings.Contains(text, v) {
			text = strings.ReplaceAll(text, v, Mask)
		}
	}
	return text
}

// Handler оборачивает обработчик журнала: сообщение и все строковые поля проходят через Set.
func Handler(next slog.Handler, s *Set) slog.Handler { return &handler{next: next, set: s} }

type handler struct {
	next slog.Handler
	set  *Set
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, h.set.String(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.clean(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = h.clean(a)
	}
	return &handler{next: h.next.WithAttrs(clean), set: h.set}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), set: h.set}
}

// clean чистит одно поле. Ошибки и любые значения превращаются в строку, иначе секрет проскочит через err.
func (h *handler) clean(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.set.String(v.String()))
	case slog.KindGroup:
		group := v.Group()
		out := make([]any, len(group))
		for i, g := range group {
			out[i] = h.clean(g)
		}
		return slog.Group(a.Key, out...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, h.set.String(err.Error()))
		}
		return slog.String(a.Key, h.set.String(v.String()))
	}
	return slog.Attr{Key: a.Key, Value: v}
}

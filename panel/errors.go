package panel

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Error это ошибка общения с панелью. Токен в тексте всегда заменён на ***.
type Error struct {
	// Status это код ответа панели, 0 если до панели не достучались.
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// IsRetryable говорит, что запрос можно повторить позже: панель не ответила,
// ответила 5xx или попросила не частить (429). Ошибки 4xx повтор не лечит.
func IsRetryable(err error) bool {
	var pe *Error
	if !errors.As(err, &pe) {
		return false
	}
	return pe.Status == 0 || pe.Status >= 500 || pe.Status == http.StatusTooManyRequests
}

// IsNotFound говорит, что панель не нашла то, о чём спросили.
func IsNotFound(err error) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.Status == http.StatusNotFound
}

// IsAuth говорит, что панель не приняла токен.
func IsAuth(err error) bool {
	var pe *Error
	return errors.As(err, &pe) && (pe.Status == http.StatusUnauthorized || pe.Status == http.StatusForbidden)
}

func (c *Client) statusError(method, path string, status int, body []byte) *Error {
	text := fmt.Sprintf("panel: %s %s: ответ %d", method, path, status)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		text += " (панель не приняла токен)"
	} else if snippet := strings.TrimSpace(string(body)); snippet != "" {
		text += ": " + cut(c.redact(snippet), 200) // сначала чистим, потом режем: обрывок токена на краю не останется
	}
	return &Error{Status: status, Msg: text}
}

func (c *Client) netError(method, path string, err error) *Error {
	text := fmt.Sprintf("panel: %s %s: панель недоступна: %s", method, path, c.redact(err.Error()))
	return &Error{Status: 0, Msg: text}
}

// redact прячет токен, если он вдруг попал в чужой текст.
func (c *Client) redact(s string) string {
	if c.token == "" {
		return s
	}
	return strings.ReplaceAll(s, c.token, "***")
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

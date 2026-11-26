package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "https://api.telegram.org"
	maxAttempts    = 3
	sendLimit      = 25 // запросов в секунду на клиента
	maxRespBytes   = 8 << 20
)

// Client это клиент Bot API. Создавать как &Client{Token: ...}, не копировать.
type Client struct {
	Token   string
	BaseURL string       // по умолчанию https://api.telegram.org
	HTTP    *http.Client // по умолчанию с таймаутом 75 секунд
	// Now и Sleep подменяются в тестах. Sleep должен вернуть ошибку, если ctx закончился.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	mu   sync.Mutex
	sent []time.Time // моменты последних отправок для лимитера
}

// Error это ошибка, которую вернул Telegram.
type Error struct {
	Code        int
	Description string
	RetryAfter  int
}

func (e *Error) Error() string {
	return fmt.Sprintf("telegram %d: %s", e.Code, e.Description)
}

// IsBlocked: пользователь заблокировал бота или удалил аккаунт.
func (e *Error) IsBlocked() bool {
	if e.Code != http.StatusForbidden {
		return false
	}
	d := strings.ToLower(e.Description)
	return strings.Contains(d, "bot was blocked by the user") || strings.Contains(d, "user is deactivated")
}

// IsNotFound: сообщение или тему править нельзя, их нет.
func (e *Error) IsNotFound() bool {
	d := strings.ToLower(e.Description)
	for _, s := range []string{"message to edit not found", "message thread not found", "message can't be edited"} {
		if strings.Contains(d, s) {
			return true
		}
	}
	return false
}

// IsNotModified: новый текст совпал со старым.
func (e *Error) IsNotModified() bool {
	return strings.Contains(strings.ToLower(e.Description), "message is not modified")
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// throttle ждёт, пока в последней секунде будет меньше sendLimit запросов, и занимает место.
func (c *Client) throttle(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		now := c.now()
		i := 0
		for i < len(c.sent) && now.Sub(c.sent[i]) >= time.Second {
			i++
		}
		c.sent = c.sent[i:]
		if len(c.sent) < sendLimit {
			c.sent = append(c.sent, now)
			return nil
		}
		if err := c.sleep(ctx, c.sent[0].Add(time.Second).Sub(now)); err != nil {
			return err
		}
	}
}

// call шлёт POST с JSON и разбирает ответ. На 429 ждёт retry_after и повторяет, всего до 3 попыток.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err = c.throttle(ctx); err != nil {
			return err
		}
		err = c.once(ctx, method, params, out)
		var te *Error
		if !errors.As(err, &te) || te.Code != http.StatusTooManyRequests || attempt == maxAttempts {
			return err
		}
		wait := time.Duration(max(te.RetryAfter, 1)) * time.Second
		if serr := c.sleep(ctx, wait); serr != nil {
			return serr
		}
	}
	return err
}

func (c *Client) once(ctx context.Context, method string, params, out any) error {
	var body io.Reader = http.NoBody
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("tg %s: кодирование запроса: %w", method, err)
		}
		body = bytes.NewReader(b)
	}
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+c.Token+"/"+method, body)
	if err != nil {
		return c.cleanErr(method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 75 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return c.cleanErr(method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))
	if err != nil {
		return c.cleanErr(method, err)
	}
	var r apiResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return &Error{Code: resp.StatusCode, Description: "ответ не JSON"}
	}
	if !r.OK {
		code := r.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &Error{Code: code, Description: r.Description, RetryAfter: r.Parameters.RetryAfter}
	}
	if out == nil || len(r.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Result, out); err != nil {
		return fmt.Errorf("tg %s: разбор результата: %w", method, err)
	}
	return nil
}

// cleanErr делает ошибку без URL и токена: net/http кладёт адрес с токеном в текст ошибки.
func (c *Client) cleanErr(method string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	msg := err.Error()
	if c.Token != "" {
		msg = strings.ReplaceAll(msg, c.Token, "***")
	}
	return errors.New("tg " + method + ": " + msg)
}

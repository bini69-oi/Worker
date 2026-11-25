package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxBody = 4 << 20

// Client говорит с одной панелью Remnawave. Безопасен для одновременного использования.
type Client struct {
	base  string
	token string
	http  *http.Client
	now   func() time.Time

	mu       sync.Mutex
	contract *contract
}

// New создаёт клиент. base это адрес панели без пути, например https://panel.example.com.
// Если http равен nil, берётся клиент с тайм-аутом 15 секунд, который не ходит по редиректам:
// иначе ключ панели в заголовке мог бы уйти по адресу, на который панель (или подменивший её) перенаправит.
func New(base, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: noRedirect}
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  httpClient,
		now:   time.Now,
	}
}

// do шлёт запрос и разбирает ответ панели. Панель кладёт данные в поле response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return &Error{Msg: "panel: плохой адрес панели: " + c.redact(err.Error())}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return c.netError(method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return c.netError(method, path, err)
	}
	if resp.StatusCode >= 400 {
		return c.statusError(method, path, resp.StatusCode, data)
	}
	return decodeResponse(method, path, data, out)
}

func decodeResponse(method, path string, data []byte, out any) error {
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var env struct {
		Response json.RawMessage `json:"response"`
	}
	payload := data
	if json.Unmarshal(data, &env) == nil && len(env.Response) > 0 {
		payload = env.Response
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return errors.New("panel: " + method + " " + path + ": непонятный ответ панели")
	}
	return nil
}

// Version спрашивает у панели её версию и запоминает, по какому контракту с ней говорить.
// Если панель версию не назвала, считаем её версией 2.x.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var meta struct {
		Version string `json:"version"`
	}
	err := c.do(ctx, http.MethodGet, "/api/system/metadata", nil, &meta)
	if err != nil && (IsAuth(err) || IsRetryable(err) || ctx.Err() != nil) {
		return Version{}, err
	}
	if meta.Version == "" {
		// Старые панели называли версию в проверке здоровья.
		if herr := c.do(ctx, http.MethodGet, "/api/system/health", nil, &meta); herr != nil && IsRetryable(herr) {
			return Version{}, herr
		}
	}
	v := parseVersion(meta.Version)
	ct := contractFor(v)
	c.mu.Lock()
	c.contract = &ct
	c.mu.Unlock()
	return v, nil
}

// current возвращает контракт панели, при первом вызове определяя версию.
func (c *Client) current(ctx context.Context) (contract, error) {
	c.mu.Lock()
	ct := c.contract
	c.mu.Unlock()
	if ct != nil {
		return *ct, nil
	}
	v, err := c.Version(ctx)
	if err != nil {
		return contract{}, err
	}
	return contractFor(v), nil
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

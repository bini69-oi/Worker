package overpay

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpClient отдаёт клиент для запроса. Заданный в New (тесты) используется как есть.
// Иначе собирается клиент с клиентским сертификатом (mTLS), если он заполнен в настройках:
// PEM читается на каждом запросе, поэтому правка в админке действует без перезапуска.
func (g *Gateway) httpClient() (*http.Client, error) {
	if g.client != nil {
		return g.client, nil
	}
	tr := &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	certPEM, keyPEM := g.cfg.Secret(keyCert), g.cfg.Secret(keyKey)
	if certPEM != "" || keyPEM != "" {
		pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return nil, errors.New("сертификат или ключ Overpay не разобраны")
		}
		tr.TLSClientConfig.Certificates = []tls.Certificate{pair}
	}
	return &http.Client{Timeout: 20 * time.Second, Transport: tr}, nil
}

// do шлёт запрос в API. Логин и пароль идут только в заголовок и в ошибки не попадают.
func (g *Gateway) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, body)
	if err != nil {
		return errors.New("не собран запрос к Overpay")
	}
	req.SetBasicAuth(g.cfg.Secret(keyUser), g.cfg.Secret(keyPass))
	req.Header.Set("Content-Type", "application/json")
	client, err := g.httpClient()
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("касса Overpay недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("касса Overpay ответила кодом %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return errors.New("ответ кассы Overpay не разобран")
	}
	return nil
}

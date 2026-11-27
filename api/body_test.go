package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bini69-oi/worker/core"
)

type buyReq struct {
	Tariff int64 `json:"tariff"`
	Months int   `json:"months"`
}

func decodeTo(body string) (buyReq, error) {
	var req buyReq
	r := httptest.NewRequest("POST", "/api/x", strings.NewReader(body))
	err := Decode(httptest.NewRecorder(), r, &req)
	return req, err
}

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"верное тело", `{"tariff":3,"months":1}`, nil},
		{"лишнее поле", `{"tariff":3,"user_id":7}`, core.ErrInvalid},
		{"не JSON", `tariff=3`, core.ErrInvalid},
		{"пустое тело", ``, core.ErrInvalid},
		{"два значения", `{"tariff":3}{"tariff":4}`, core.ErrInvalid},
		{"мусор после значения", `{"tariff":3} x`, core.ErrInvalid},
		{"не тот тип", `{"tariff":"три"}`, core.ErrInvalid},
		{"массив вместо объекта", `[1]`, core.ErrInvalid},
		{"пустой объект", `{}`, nil},
		{"пробелы после значения", "{\"tariff\":3}\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := decodeTo(c.body)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("ошибка %v, ждали %v", err, c.want)
			}
		})
	}
}

func TestDecodeBigBody(t *testing.T) {
	s := newTestServer(t)
	read := 0
	s.Handle("POST /api/x", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req buyReq
		err := Decode(w, r, &req)
		return nil, err
	})
	// 1 МБ тела: JSON верный по форме, но огромный.
	big := `{"tariff":1,"months":1,"pad":"` + strings.Repeat("a", 1<<20) + `"}`
	counter := &countReader{r: strings.NewReader(big), n: &read}
	req := httptest.NewRequest("POST", "/api/x", counter)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 413 || !strings.Contains(rec.Body.String(), "E1004") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if read > 64<<10 {
		t.Errorf("прочитано %d байт: тело читается целиком", read)
	}
}

func TestDecodeLimitEdge(t *testing.T) {
	pad := func(n int) string {
		head, tail := `{"tariff":1,"months":1}`, ""
		return head + strings.Repeat(" ", n-len(head)) + tail
	}
	if _, err := decodeTo(pad(maxBody)); err != nil {
		t.Errorf("тело ровно в предел отклонено: %v", err)
	}
	if _, err := decodeTo(pad(maxBody + 1)); !errors.Is(err, core.ErrTooLong) {
		t.Errorf("тело на байт больше предела: %v", err)
	}
}

type countReader struct {
	r interface{ Read([]byte) (int, error) }
	n *int
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	*c.n += n
	return n, err
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{``, `{}`, `{"tariff":1}`, `{"user_id":1}`, `[`, `{"tariff":1}{`, "\x00", `{"tariff":1e999}`, `null`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		var req buyReq
		r := httptest.NewRequest("POST", "/api/x", bytes.NewReader(body))
		err := Decode(httptest.NewRecorder(), r, &req)
		if err != nil {
			if !errors.Is(err, core.ErrInvalid) && !errors.Is(err, core.ErrTooLong) {
				t.Fatalf("ошибка без кода ядра: %v", err)
			}
			return
		}
		// Принятое тело это один объект только с известными полями.
		var raw map[string]json.RawMessage
		if json.Unmarshal(body, &raw) != nil {
			t.Fatalf("принято не-объект: %q", body)
		}
		for k := range raw {
			if k != "tariff" && k != "months" {
				t.Fatalf("принято неизвестное поле %q", k)
			}
		}
	})
}

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/bini69-oi/worker/texts"
)

// memTexts это хранилище правок текстов в памяти: правок нет, значит отдаются тексты из кода.
type memTexts struct{}

func (memTexts) LoadAll() ([]texts.Row, error)      { return nil, nil }
func (memTexts) Save(_, _, _ string, _ int64) error { return nil }
func (memTexts) Delete(_, _ string) error           { return nil }

// realTextsServer это тот же кабинет, но с настоящими текстами из пакета texts.
func realTextsServer(t *testing.T, e *appEnv) *Server {
	t.Helper()
	tx, err := texts.New(memTexts{}, func() string { return "ru" }, e.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Deps{Text: func(key, lang string) string { return tx.Render(key, lang, nil) },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: e.clock.Now})
	RegisterApp(s, AppDeps{Core: e.c, Config: e.cfg, BotToken: func() string { return testBotToken },
		BotName: func() string { return "worker_bot" }, PublicURL: "https://w.test", DLKey: []byte(strings.Repeat("k", 32)),
		Texts: tx.ByPrefix})
	return s
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// TestEnglishCabinet: абонент с языком en получает английские тексты кабинета и ошибок и главную с lang en.
// Эталоны texts_ru, texts_en и me_en берёт фронт: его тесты проверяют, что на английском нет русских строк.
func TestEnglishCabinet(t *testing.T) {
	e := newAppEnv(t)
	s := realTextsServer(t, e)
	do := func(method, target, body string, hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.RemoteAddr = "10.0.0.1:1"
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}

	b, _ := json.Marshal(map[string]string{"init_data": initData(91, "en-GB", e.clock.Now())})
	rec := do("POST", "/api/app/session/tg", string(b), "Content-Type", "application/json")
	var sess SessionResponse
	decode(t, rec, &sess)
	if sess.Lang != "en" {
		t.Fatalf("язык нового абонента из Telegram: %+v", sess)
	}
	auth := []string{"Authorization", "Bearer " + sess.Token}

	me := do("GET", "/api/app/me", "", auth...)
	golden(t, "me_en", me.Body.Bytes())

	en := do("GET", "/api/app/texts", "", auth...)
	var out TextsResponse
	decode(t, en, &out)
	if out.Lang != "en" || len(out.Texts) < 100 {
		t.Fatalf("английские тексты: %s, %d ключей", out.Lang, len(out.Texts))
	}
	for k, v := range out.Texts {
		if hasCyrillic(v) {
			t.Errorf("английский текст %s содержит кириллицу: %q", k, v)
		}
	}
	golden(t, "texts_en_app", en.Body.Bytes())

	ru := do("GET", "/api/app/texts?lang=ru", "")
	golden(t, "texts_ru_app", ru.Body.Bytes())

	// Ошибки тоже на языке абонента.
	rec = do("POST", "/api/app/buy", `{"tariff_id":999,"days":30}`, append(auth, "Content-Type", "application/json")...)
	var bad ErrorBody
	decode(t, rec, &bad)
	if bad.Code != "E1001" || bad.Text != "Not found" {
		t.Fatalf("ошибка на английском: %+v", bad)
	}
}

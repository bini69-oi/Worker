package tgsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testToken = "123456:TEST-token"
	testUser  = `{"id":42,"first_name":"Ivan","username":"ivan","language_code":"ru"}`
	// Хеши посчитаны один раз отдельным инструментом, не этим кодом.
	fixedMiniAppHash = "012b286a8db80d8f327606ee37dd5d99cf6bbc6e0b604661282884da0f5ca1f2"
	fixedWidgetHash  = "e44cd32a7373654958e6d136c8a2320cd1952852cd531c4d4c267308bf4146bd"
)

var fixedNow = time.Unix(1700000000, 0).Add(10 * time.Minute)

// signTest это независимый хелпер: подпись считается заново, без функций пакета.
func signTest(key []byte, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+fields[k])
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(m.Sum(nil))
}

func miniAppKey(token string) []byte {
	m := hmac.New(sha256.New, []byte("WebAppData"))
	m.Write([]byte(token))
	return m.Sum(nil)
}

func buildInitData(fields map[string]string, hash string) string {
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	if hash != "" {
		v.Set("hash", hash)
	}
	return v.Encode()
}

func authDate(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) }

func TestWebhook(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		set      bool
		expected string
		want     error
	}{
		{"без заголовка", "", false, "sekret", ErrBadSecret},
		{"верный секрет", "sekret", true, "sekret", nil},
		{"неверный секрет", "other", true, "sekret", ErrBadSecret},
		{"пустой ожидаемый и пустой заголовок", "", true, "", ErrNoSecret},
		{"пустой ожидаемый, заголовок есть", "x", true, "", ErrNoSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/hook", nil)
			if tt.set {
				r.Header.Set(WebhookSecretHeader, tt.header)
			}
			if got := CheckWebhook(r, tt.expected); !errors.Is(got, tt.want) {
				t.Fatalf("получили %v, ждали %v", got, tt.want)
			}
		})
	}
}

func TestInitData(t *testing.T) {
	now := fixedNow
	fresh := map[string]string{"auth_date": authDate(now.Add(-5 * time.Minute)), "user": testUser, "query_id": "AAH"}
	old := map[string]string{"auth_date": authDate(now.Add(-2 * time.Hour)), "user": testUser}
	future := map[string]string{"auth_date": authDate(now.Add(5 * time.Minute)), "user": testUser}
	noUser := map[string]string{"auth_date": authDate(now)}
	badUser := map[string]string{"auth_date": authDate(now), "user": "не json"}
	noDate := map[string]string{"user": testUser}

	forged := map[string]string{"auth_date": fresh["auth_date"], "user": strings.Replace(testUser, `"id":42`, `"id":1`, 1), "query_id": "AAH"}

	tests := []struct {
		name string
		data string
		want error
	}{
		{"верная подпись", buildInitData(fresh, signTest(miniAppKey(testToken), fresh)), nil},
		{"подделан Telegram ID", buildInitData(forged, signTest(miniAppKey(testToken), fresh)), ErrBadSignature},
		{"чужой токен", buildInitData(fresh, signTest(miniAppKey("1:other"), fresh)), ErrBadSignature},
		{"старше часа", buildInitData(old, signTest(miniAppKey(testToken), old)), ErrExpired},
		{"из будущего", buildInitData(future, signTest(miniAppKey(testToken), future)), ErrFromFuture},
		{"нет hash", buildInitData(fresh, ""), ErrNoHash},
		{"нет auth_date", buildInitData(noDate, signTest(miniAppKey(testToken), noDate)), ErrNoAuthDate},
		{"нет user", buildInitData(noUser, signTest(miniAppKey(testToken), noUser)), ErrNoUser},
		{"user не json", buildInitData(badUser, signTest(miniAppKey(testToken), badUser)), ErrBadFormat},
		{"повторный ключ", "a=1&a=2&hash=zz", ErrBadFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerifyInitData(tt.data, testToken, now, 0)
			if !errors.Is(err, tt.want) {
				t.Fatalf("получили %v, ждали %v", err, tt.want)
			}
		})
	}
}

func TestInitDataParse(t *testing.T) {
	f := map[string]string{"auth_date": "1700000000", "user": testUser, "query_id": "AAH", "start_param": "ref1"}
	data := buildInitData(f, signTest(miniAppKey(testToken), f))
	got, err := VerifyInitData(data, testToken, fixedNow, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := User{ID: 42, FirstName: "Ivan", Username: "ivan", LanguageCode: "ru"}
	if got.User != want || got.StartParam != "ref1" || got.AuthDate.Unix() != 1700000000 {
		t.Fatalf("разобрано неверно: %+v", got)
	}
}

func TestInitDataFixedHash(t *testing.T) {
	f := map[string]string{"auth_date": "1700000000", "user": testUser, "query_id": "AAH", "start_param": "ref1"}
	if got := signTest(miniAppKey(testToken), f); got != fixedMiniAppHash {
		t.Fatalf("хелпер даёт %s, ждали %s", got, fixedMiniAppHash)
	}
	if _, err := VerifyInitData(buildInitData(f, fixedMiniAppHash), testToken, fixedNow, 0); err != nil {
		t.Fatal(err)
	}
}

func TestCustomMaxAge(t *testing.T) {
	f := map[string]string{"auth_date": authDate(fixedNow.Add(-10 * time.Minute)), "user": testUser}
	data := buildInitData(f, signTest(miniAppKey(testToken), f))
	if _, err := VerifyInitData(data, testToken, fixedNow, 5*time.Minute); !errors.Is(err, ErrExpired) {
		t.Fatalf("ждали ErrExpired, получили %v", err)
	}
	if _, err := VerifyInitData(data, testToken, fixedNow, 20*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestLogin(t *testing.T) {
	key := sha256.Sum256([]byte(testToken))
	now := fixedNow
	mk := func(date time.Time) map[string]string {
		return map[string]string{"auth_date": authDate(date), "first_name": "Ivan", "id": "42", "username": "ivan"}
	}
	fresh, old := mk(now.Add(-time.Minute)), mk(now.Add(-3*time.Hour))
	forged := mk(now.Add(-time.Minute))
	forged["id"] = "1"

	tests := []struct {
		name   string
		fields map[string]string
		hash   string
		want   error
	}{
		{"верная подпись", fresh, signTest(key[:], fresh), nil},
		{"подделан id", forged, signTest(key[:], fresh), ErrBadSignature},
		{"старше часа", old, signTest(key[:], old), ErrExpired},
		{"нет hash", fresh, "", ErrNoHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := url.ParseQuery(buildInitData(tt.fields, tt.hash))
			u, err := VerifyLogin(v, testToken, now, 0)
			if !errors.Is(err, tt.want) {
				t.Fatalf("получили %v, ждали %v", err, tt.want)
			}
			if err == nil && (u.ID != 42 || u.Username != "ivan" || u.FirstName != "Ivan") {
				t.Fatalf("пользователь разобран неверно: %+v", u)
			}
		})
	}
}

func TestLoginFixedHash(t *testing.T) {
	v := url.Values{}
	v.Set("auth_date", "1700000000")
	v.Set("first_name", "Ivan")
	v.Set("id", "42")
	v.Set("username", "ivan")
	v.Set("hash", fixedWidgetHash)
	if _, err := VerifyLogin(v, testToken, fixedNow, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTokenNotInError(t *testing.T) {
	_, err := VerifyInitData("auth_date=1&hash=zz", testToken, fixedNow, 0)
	if err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("ошибка пуста или содержит токен: %v", err)
	}
}

func TestEmptyToken(t *testing.T) {
	now := time.Unix(1700000000, 0)
	if _, err := VerifyInitData("auth_date=1700000000&hash=00", "", now, 0); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Mini App без токена: ждали ErrNoToken, получили %v", err)
	}
	if _, err := VerifyLogin(url.Values{"id": {"1"}, "auth_date": {"1700000000"}, "hash": {"00"}}, "", now, 0); !errors.Is(err, ErrNoToken) {
		t.Fatalf("виджет без токена: ждали ErrNoToken, получили %v", err)
	}
}

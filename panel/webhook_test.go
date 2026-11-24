package panel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testSecret = "s3cretKeyForTestsOnly0123456789abcdefghijklmnopqrstuvwxyzABCDEF"

func sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func hookRequest(body, sig, ts string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/hook", strings.NewReader(body))
	if sig != "" {
		r.Header.Set(HeaderSignature, sig)
	}
	if ts != "" {
		r.Header.Set(HeaderTimestamp, ts)
	}
	return r
}

func TestVerifyWebhook(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	body := `{"scope":"user","event":"user.expired"}`
	good := sign(testSecret, []byte(body))
	fresh := now.Add(-time.Minute).Format(time.RFC3339Nano)
	old := now.Add(-time.Hour).Format(time.RFC3339Nano)

	cases := []struct {
		name   string
		secret string
		sig    string
		ts     string
		want   error
	}{
		{"верная подпись", testSecret, good, fresh, nil},
		{"без метки времени", testSecret, good, "", nil},
		{"неверная подпись", testSecret, sign("other", []byte(body)), fresh, ErrWebhookBadSignature},
		{"не hex", testSecret, "zz", fresh, ErrWebhookBadSignature},
		{"пустой секрет", "", good, fresh, ErrWebhookNoSecret},
		{"без заголовка", testSecret, "", fresh, ErrWebhookNoSignature},
		{"старая метка", testSecret, good, old, ErrWebhookStale},
		{"битая метка", testSecret, good, "вчера", ErrWebhookBadTimestamp},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := VerifyWebhook(hookRequest(body, c.sig, c.ts), c.secret, now)
			if !errors.Is(err, c.want) {
				t.Fatalf("ошибка %v, ждали %v", err, c.want)
			}
			if c.want == nil && string(got) != body {
				t.Fatalf("тело %q", got)
			}
		})
	}
}

func TestVerifyWebhookTooLarge(t *testing.T) {
	body := strings.Repeat("a", MaxWebhookBody+10)
	_, err := VerifyWebhook(hookRequest(body, sign(testSecret, []byte(body)), ""), testSecret, time.Now())
	if !errors.Is(err, ErrWebhookTooLarge) {
		t.Fatalf("ошибка %v", err)
	}
}

func TestErrorsHideSecret(t *testing.T) {
	now := time.Now()
	for _, sig := range []string{"", "00ff", sign("other", []byte("x"))} {
		_, err := VerifyWebhook(hookRequest("x", sig, now.Add(-time.Hour).Format(time.RFC3339)), testSecret, now)
		if err == nil || strings.Contains(err.Error(), testSecret) {
			t.Fatalf("ошибка %v", err)
		}
	}
}

const bodyV3 = `{"scope":"user","event":"user.limited","timestamp":"2026-11-25T11:59:00.000Z",
"data":{"id":42,"uuid":"11111111-2222-3333-4444-555555555555","username":"tg_7","status":"LIMITED",
"trafficLimitBytes":1000,"telegramId":777,"expireAt":"2026-12-01T00:00:00.000Z",
"userTraffic":{"usedTrafficBytes":1500,"lifetimeUsedTrafficBytes":9000}},"meta":null}`

const bodyV2 = `{"scope":"user","event":"user.expired","timestamp":"2026-11-25T11:59:00.000Z",
"data":{"uuid":"aaaaaaaa-2222-3333-4444-555555555555","username":"tg_8","status":"EXPIRED",
"trafficLimitBytes":0,"telegramId":null,"expireAt":"2026-11-24T00:00:00.000Z",
"usedTrafficBytes":0,"userTraffic":{"usedTrafficBytes":12}}}`

func TestParseEventV3(t *testing.T) {
	ev, err := ParseEvent([]byte(bodyV3))
	if err != nil {
		t.Fatal(err)
	}
	u := ev.User
	if ev.Scope != ScopeUser || ev.Name != EventUserLimited || u == nil {
		t.Fatalf("событие %+v", ev)
	}
	if u.ID != 42 || u.UUID == "" || u.Username != "tg_7" || u.TelegramID != 777 ||
		u.Status != "LIMITED" || u.UsedTrafficBytes != 1500 || u.TrafficLimitBytes != 1000 {
		t.Fatalf("абонент %+v", u)
	}
	if !u.ExpireAt.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) || ev.At.IsZero() {
		t.Fatalf("время %v %v", u.ExpireAt, ev.At)
	}
}

func TestParseEventV2(t *testing.T) {
	ev, err := ParseEvent([]byte(bodyV2))
	if err != nil {
		t.Fatal(err)
	}
	u := ev.User
	if ev.Name != EventUserExpired || u == nil || u.ID != 0 || u.UUID == "" ||
		u.TelegramID != 0 || u.UsedTrafficBytes != 12 {
		t.Fatalf("событие %+v %+v", ev, u)
	}
}

func TestParseEventUnknown(t *testing.T) {
	ev, err := ParseEvent([]byte(`{"scope":"node","event":"node.connection_lost","timestamp":"2026-11-25T11:59:00Z","data":{"name":"n1"}}`))
	if err != nil || ev.Scope != ScopeNode || ev.User != nil {
		t.Fatalf("%+v %v", ev, err)
	}
	ev, err = ParseEvent([]byte(`{"scope":"user","event":"user.something_new","timestamp":"2026-11-25T11:59:00Z","data":{"username":"x"}}`))
	if err != nil || ev.Name != "user.something_new" || ev.User == nil {
		t.Fatalf("%+v %v", ev, err)
	}
}

func TestParseEventBad(t *testing.T) {
	for _, b := range []string{"", "{", `{"scope":"user"}`} {
		if _, err := ParseEvent([]byte(b)); !errors.Is(err, ErrWebhookBadBody) {
			t.Fatalf("%q: %v", b, err)
		}
	}
}

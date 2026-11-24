package panel_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bini69-oi/worker/core"
	"github.com/bini69-oi/worker/core/coretest"
	"github.com/bini69-oi/worker/panel"
)

const hookSecret = "hook-secret-0123456789"

func signedRequest(body, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/panel/webhook", strings.NewReader(body))
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	r.Header.Set(panel.HeaderSignature, hex.EncodeToString(m.Sum(nil)))
	return r
}

func TestWebhookHandler(t *testing.T) {
	st := coretest.NewStore()
	c := core.New(core.Deps{Store: st, Clock: coretest.NewClock(time.Now()), Config: coretest.NewConfig(t)})
	h := panel.WebhookHandler(c, func() string { return hookSecret }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `{"scope":"node","event":"node.connection_lost","timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","data":{}}`

	w := httptest.NewRecorder()
	h.ServeHTTP(w, signedRequest(body, "wrong-secret"))
	if w.Code != http.StatusUnauthorized || len(st.Tasks()) != 0 {
		t.Fatalf("неверная подпись: код %d, задач %d", w.Code, len(st.Tasks()))
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, signedRequest(body, hookSecret))
	if w.Code != http.StatusOK || st.Pending(core.KindNotifyAdmin) != 1 {
		t.Fatalf("верная подпись: код %d, задач админу %d", w.Code, st.Pending(core.KindNotifyAdmin))
	}
}

func TestWebhookEmptySecretRejected(t *testing.T) {
	st := coretest.NewStore()
	c := core.New(core.Deps{Store: st, Config: coretest.NewConfig(t)})
	h := panel.WebhookHandler(c, func() string { return "" }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, signedRequest(`{"scope":"node","event":"x"}`, ""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("пустой секрет должен закрывать вебхук, код %d", w.Code)
	}
}

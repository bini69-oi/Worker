package bot

import (
	"context"
	"strings"
	"testing"
)

// Сообщение от админа экранируется один раз: абонент видит свои знаки, а не «&amp;amp;».
func TestSupportReplyEscapedOnce(t *testing.T) {
	e := newEnv(t)
	u := e.start(t)
	if err := e.c.SendToUser(context.Background(), owner, u.ID, `Ответ "да" & <ok>`); err != nil {
		t.Fatal(err)
	}
	e.sync(t)
	seen := false
	for _, c := range e.api.take() {
		if c.chat != userTg {
			continue
		}
		seen = true
		if strings.Contains(c.text, "&amp;amp;") || strings.Contains(c.text, "&amp;lt;") {
			t.Errorf("двойное экранирование: %q", c.text)
		}
		if !strings.Contains(c.text, "&amp; &lt;ok&gt;") {
			t.Errorf("разметка абонента не экранирована один раз: %q", c.text)
		}
	}
	if !seen {
		t.Error("сообщение не дошло")
	}
}

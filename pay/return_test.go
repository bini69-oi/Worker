package pay

import (
	"strconv"
	"testing"
)

func TestReturnURL(t *testing.T) {
	def := func(id int64) string { return "https://t.me/bot?start=paid_" + strconv.FormatInt(id, 10) }
	if got := ReturnURL("", def, 42); got != "https://t.me/bot?start=paid_42" {
		t.Fatalf("по умолчанию: %s", got)
	}
	if got := ReturnURL("https://site/ok?p={payment}", def, 42); got != "https://site/ok?p=42" {
		t.Fatalf("из настройки: %s", got)
	}
	if got := ReturnURL("", nil, 42); got != "" {
		t.Fatalf("без адреса: %s", got)
	}
}

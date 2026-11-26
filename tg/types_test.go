package tg

import (
	"encoding/json"
	"os"
	"testing"
)

func loadUpdate(t *testing.T, name string) Update {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var u Update
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return u
}

func TestParseUpdates(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		check func(u Update) bool
	}{
		{"текст в теме с ответом", "update_message.json", func(u Update) bool {
			m := u.Message
			return u.UpdateID == 1001 && m.MessageID == 55 && m.ThreadID == 7 && m.IsTopicMessage &&
				m.From.ID == 4242 && m.Chat.ID == -1001234 && m.Chat.IsForum && m.Text == "/start ref_9" &&
				m.ReplyToMessage != nil && m.ReplyToMessage.MessageID == 50
		}},
		{"фото", "update_photo.json", func(u Update) bool {
			return u.Message.HasMedia() && u.Message.Caption == "чек" && u.Message.Text == ""
		}},
		{"кнопка", "update_callback.json", func(u Update) bool {
			q := u.CallbackQuery
			return q.ID == "cb1" && q.Data == "buy:3:30" && q.From.ID == 4242 && q.Message.MessageID == 60
		}},
		{"перед оплатой", "update_precheckout.json", func(u Update) bool {
			q := u.PreCheckoutQuery
			return q.ID == "pc1" && q.Currency == "XTR" && q.TotalAmount == 150 && q.InvoicePayload == "order:77"
		}},
		{"оплата", "update_payment.json", func(u Update) bool {
			p := u.Message.SuccessfulPayment
			return p != nil && p.TelegramPaymentChargeID == "chg_1" && p.IsRecurring && p.IsFirstRecurring &&
				p.SubscriptionExpirationDate == 1702592000 && p.TotalAmount == 150
		}},
		{"возврат", "update_refund.json", func(u Update) bool {
			p := u.Message.RefundedPayment
			return p != nil && p.TelegramPaymentChargeID == "chg_1" && p.InvoicePayload == "order:77"
		}},
		{"бот заблокирован", "update_my_chat_member.json", func(u Update) bool {
			m := u.MyChatMember
			return m.OldChatMember.Status == "member" && m.NewChatMember.Status == "kicked" && m.From.ID == 4242
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if u := loadUpdate(t, tc.file); !tc.check(u) {
				t.Fatalf("поля разобраны неверно: %+v", u)
			}
		})
	}
}

func TestKeyboardJSON(t *testing.T) {
	m := InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
		{Text: "a", CallbackData: "x"},
		{Text: "b", WebApp: &WebAppInfo{URL: "https://w"}},
		{Text: "c", CopyText: &CopyTextButton{Text: "key"}},
	}}}
	b, _ := json.Marshal(m)
	want := `{"inline_keyboard":[[{"text":"a","callback_data":"x"},{"text":"b","web_app":{"url":"https://w"}},{"text":"c","copy_text":{"text":"key"}}]]}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

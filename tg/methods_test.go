package tg

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMethods(t *testing.T) {
	ctx := context.Background()
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "ok", CallbackData: "m"}}}}
	prices := []LabeledPrice{{Label: "VPN", Amount: 150}}
	cases := []struct {
		name     string
		method   string
		response string
		wantBody string
		call     func(c *Client) (any, error)
		want     any
	}{
		{"getMe", "getMe", `{"ok":true,"result":{"id":5,"username":"b"}}`, ``,
			func(c *Client) (any, error) { return c.GetMe(ctx) }, User{ID: 5, Username: "b"}},
		{"setWebhook", "setWebhook", `{"ok":true,"result":true}`,
			`{"url":"https://x/h","secret_token":"s","allowed_updates":["message","callback_query"]}`,
			func(c *Client) (any, error) {
				return nil, c.SetWebhook(ctx, "https://x/h", "s", []string{"message", "callback_query"})
			}, nil},
		{"deleteWebhook", "deleteWebhook", `{"ok":true,"result":true}`, ``,
			func(c *Client) (any, error) { return nil, c.DeleteWebhook(ctx) }, nil},
		{"getUpdates", "getUpdates", `{"ok":true,"result":[{"update_id":8},{"update_id":9}]}`,
			`{"offset":8,"timeout":30}`,
			func(c *Client) (any, error) { return c.GetUpdates(ctx, 8, 30) },
			[]Update{{UpdateID: 8}, {UpdateID: 9}}},
		{"sendMessage", "sendMessage", `{"ok":true,"result":{"message_id":70,"chat":{"id":1}}}`,
			`{"chat_id":1,"message_thread_id":4,"text":"hi","parse_mode":"HTML","reply_markup":{"inline_keyboard":[[{"text":"ok","callback_data":"m"}]]},"reply_parameters":{"message_id":3,"allow_sending_without_reply":true},"link_preview_options":{"is_disabled":true}}`,
			func(c *Client) (any, error) {
				m, err := c.SendMessage(ctx, SendParams{ChatID: 1, ThreadID: 4, Text: "hi", ParseMode: "HTML", Markup: kb, ReplyTo: 3, DisablePreview: true})
				return m.MessageID, err
			}, 70},
		{"sendMessage минимум", "sendMessage", `{"ok":true,"result":{"message_id":71,"chat":{"id":1}}}`,
			`{"chat_id":1,"text":"hi"}`,
			func(c *Client) (any, error) {
				m, err := c.SendMessage(ctx, SendParams{ChatID: 1, Text: "hi"})
				return m.MessageID, err
			}, 71},
		{"editMessageText", "editMessageText", `{"ok":true,"result":true}`,
			`{"chat_id":1,"message_id":2,"text":"new","parse_mode":"HTML","reply_markup":{"inline_keyboard":[[{"text":"ok","callback_data":"m"}]]}}`,
			func(c *Client) (any, error) {
				return nil, c.EditMessageText(ctx, EditParams{ChatID: 1, MessageID: 2, Text: "new", ParseMode: "HTML", Markup: kb})
			}, nil},
		{"answerCallbackQuery", "answerCallbackQuery", `{"ok":true,"result":true}`,
			`{"callback_query_id":"q","text":"t","show_alert":true}`,
			func(c *Client) (any, error) { return nil, c.AnswerCallbackQuery(ctx, "q", "t", true) }, nil},
		{"copyMessage", "copyMessage", `{"ok":true,"result":{"message_id":88}}`,
			`{"chat_id":-100,"message_thread_id":6,"from_chat_id":4242,"message_id":5,"reply_parameters":{"message_id":2,"allow_sending_without_reply":true}}`,
			func(c *Client) (any, error) { return c.CopyMessage(ctx, -100, 6, 4242, 5, 2) }, 88},
		{"sendInvoice", "sendInvoice", `{"ok":true,"result":{"message_id":90,"chat":{"id":4242}}}`,
			`{"chat_id":4242,"title":"T","description":"D","payload":"p","currency":"XTR","prices":[{"label":"VPN","amount":150}]}`,
			func(c *Client) (any, error) {
				m, err := c.SendInvoice(ctx, InvoiceParams{ChatID: 4242, Title: "T", Description: "D", Payload: "p", Currency: "XTR", Prices: prices})
				return m.MessageID, err
			}, 90},
		{"createInvoiceLink", "createInvoiceLink", `{"ok":true,"result":"https://t.me/$abc"}`,
			`{"title":"T","description":"D","payload":"p","currency":"XTR","prices":[{"label":"VPN","amount":150}],"subscription_period":2592000}`,
			func(c *Client) (any, error) {
				return c.CreateInvoiceLink(ctx, InvoiceParams{Title: "T", Description: "D", Payload: "p", Currency: "XTR", Prices: prices, SubscriptionPeriod: 2592000})
			}, "https://t.me/$abc"},
		{"answerPreCheckoutQuery ok", "answerPreCheckoutQuery", `{"ok":true,"result":true}`,
			`{"pre_checkout_query_id":"pc","ok":true}`,
			func(c *Client) (any, error) { return nil, c.AnswerPreCheckoutQuery(ctx, "pc", true, "") }, nil},
		{"answerPreCheckoutQuery отказ", "answerPreCheckoutQuery", `{"ok":true,"result":true}`,
			`{"pre_checkout_query_id":"pc","ok":false,"error_message":"нет"}`,
			func(c *Client) (any, error) { return nil, c.AnswerPreCheckoutQuery(ctx, "pc", false, "нет") }, nil},
		{"refundStarPayment", "refundStarPayment", `{"ok":true,"result":true}`,
			`{"user_id":4242,"telegram_payment_charge_id":"chg"}`,
			func(c *Client) (any, error) { return nil, c.RefundStarPayment(ctx, 4242, "chg") }, nil},
		{"editUserStarSubscription", "editUserStarSubscription", `{"ok":true,"result":true}`,
			`{"user_id":4242,"telegram_payment_charge_id":"chg","is_canceled":true}`,
			func(c *Client) (any, error) { return nil, c.EditUserStarSubscription(ctx, 4242, "chg", true) }, nil},
		{"setMyCommands", "setMyCommands", `{"ok":true,"result":true}`,
			`{"commands":[{"command":"start","description":"Меню"}],"language_code":"ru"}`,
			func(c *Client) (any, error) {
				return nil, c.SetMyCommands(ctx, []BotCommand{{Command: "start", Description: "Меню"}}, "ru")
			}, nil},
		{"setChatMenuButton web_app", "setChatMenuButton", `{"ok":true,"result":true}`,
			`{"menu_button":{"type":"web_app","text":"Кабинет","web_app":{"url":"https://w"}}}`,
			func(c *Client) (any, error) { return nil, c.SetChatMenuButton(ctx, 0, "Кабинет", "https://w") }, nil},
		{"setChatMenuButton default", "setChatMenuButton", `{"ok":true,"result":true}`,
			`{"chat_id":4242,"menu_button":{"type":"default"}}`,
			func(c *Client) (any, error) { return nil, c.SetChatMenuButton(ctx, 4242, "", "") }, nil},
		{"createForumTopic", "createForumTopic", `{"ok":true,"result":{"message_thread_id":12,"name":"Ivan"}}`,
			`{"chat_id":-100,"name":"Ivan"}`,
			func(c *Client) (any, error) { return c.CreateForumTopic(ctx, -100, "Ivan") }, 12},
		{"getChatMember", "getChatMember", `{"ok":true,"result":{"status":"administrator","user":{"id":4242}}}`,
			`{"chat_id":"-100","user_id":4242}`,
			func(c *Client) (any, error) { return c.GetChatMember(ctx, "-100", 4242) }, "administrator"},
		{"leaveChat", "leaveChat", `{"ok":true,"result":true}`, `{"chat_id":-100}`,
			func(c *Client) (any, error) { return nil, c.LeaveChat(ctx, -100) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				gotPath, gotBody = r.URL.Path, string(b)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			got, err := tc.call(newTestClient(srv))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(gotPath, "/"+tc.method) {
				t.Fatalf("путь %s", gotPath)
			}
			if tc.wantBody != "" && !sameJSON(t, gotBody, tc.wantBody) {
				t.Fatalf("тело\n got %s\nwant %s", gotBody, tc.wantBody)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("результат %#v, ждали %#v", got, tc.want)
			}
		})
	}
}

func sameJSON(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatalf("тело не JSON: %q", a)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

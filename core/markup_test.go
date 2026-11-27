package core_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bini69-oi/worker/core"
)

func TestCheckMarkup(t *testing.T) {
	cases := []struct {
		name, text string
		want       error  // nil, ErrBadMarkup или ErrTooLong
		detail     string // что должно быть в пояснении
	}{
		{"простой текст", "Привет, всем хорошего дня", nil, ""},
		{"все разрешённые теги", "<b>б</b><i>к</i><u>п</u><s>з</s><code>к</code><pre>п</pre><blockquote>ц</blockquote>", nil, ""},
		{"вложенные теги", "<b>жирный <i>и курсив</i></b>", nil, ""},
		{"код в pre с языком", `<pre><code class="language-go">x := 1</code></pre>`, nil, ""},
		{"ссылка https", `<a href="https://example.com/a?b=1&amp;c=2">сайт</a>`, nil, ""},
		{"ссылка tg", `<a href="tg://user?id=123">человек</a>`, nil, ""},
		{"сворачиваемая цитата", "<blockquote expandable>длинно</blockquote>", nil, ""},
		{"экранированные знаки", "1 &lt; 2 &amp;&amp; 3 &gt; 2 &quot;да&quot;", nil, ""},
		{"чужой тег script", "до <script>alert(1)</script> после", core.ErrBadMarkup, "script"},
		{"чужой тег img", `<img src="x" onerror="alert(1)">`, core.ErrBadMarkup, "img"},
		{"чужой тег в верхнем регистре", "<SCRIPT>x</SCRIPT>", core.ErrBadMarkup, "script"},
		{"ссылка javascript", `<a href="javascript:alert(1)">x</a>`, core.ErrBadMarkup, "https"},
		{"ссылка javascript в регистре", `<a href="JaVaScRiPt:alert(1)">x</a>`, core.ErrBadMarkup, "https"},
		{"ссылка javascript в сущностях", `<a href="&#106;avascript:alert(1)">x</a>`, core.ErrBadMarkup, "https"},
		{"ссылка с пробелом и переводом строки", "<a href=\" \njavascript:alert(1)\">x</a>", core.ErrBadMarkup, "https"},
		{"ссылка data", `<a href="data:text/html;base64,AAAA">x</a>`, core.ErrBadMarkup, "https"},
		{"ссылка http", `<a href="http://example.com">x</a>`, core.ErrBadMarkup, "https"},
		{"ссылка без адреса", `<a href="https://">x</a>`, core.ErrBadMarkup, "https"},
		{"ссылка без href", "<a>x</a>", core.ErrBadMarkup, "href"},
		{"чужой атрибут", `<b onclick="x()">x</b>`, core.ErrBadMarkup, "onclick"},
		{"стиль в теге", `<b style="color:red">x</b>`, core.ErrBadMarkup, "style"},
		{"тег не закрыт", "<b>жирный", core.ErrBadMarkup, "не закрыт"},
		{"лишний закрывающий", "жирный</b>", core.ErrBadMarkup, "лишний"},
		{"неверный порядок", "<b><i>x</b></i>", core.ErrBadMarkup, "не закрыт"},
		{"самозакрывающийся", "<b/>", core.ErrBadMarkup, "b"},
		{"голый знак меньше", "если a < b то", core.ErrBadMarkup, "&lt;"},
		{"знак меньше в конце", "a <", core.ErrBadMarkup, "&lt;"},
		{"неизвестная сущность", "a &nbsp; b", core.ErrBadMarkup, "&"},
		{"голый амперсанд", "a & b", core.ErrBadMarkup, "&"},
		{"пусто", "   ", core.ErrBadMarkup, "пуст"},
		{"ровно 4096", strings.Repeat("я", 4096), nil, ""},
		{"4097", strings.Repeat("я", 4097), core.ErrTooLong, ""},
		{"слишком глубоко", strings.Repeat("<b>", 40) + strings.Repeat("</b>", 40), core.ErrBadMarkup, "вложен"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := core.CheckMarkup(c.text)
			if c.want == nil {
				if err != nil {
					t.Fatalf("ждали успех, получили %v", err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("ждали %v, получили %v", c.want, err)
			}
			var d interface{ Detail() string }
			if c.detail != "" && (!errors.As(err, &d) || !strings.Contains(strings.ToLower(d.Detail()), strings.ToLower(c.detail))) {
				t.Errorf("пояснение %q не называет %q", err, c.detail)
			}
		})
	}
}

func TestCheckButtonURL(t *testing.T) {
	for _, ok := range []string{"https://example.com", "tg://resolve?domain=bot", "https://t.me/bot?start=x"} {
		if err := core.CheckButtonURL(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "http://example.com", "javascript:alert(1)", "ftp://x", "https://", "//example.com", " https://example.com", "https://exa mple.com"} {
		if err := core.CheckButtonURL(bad); !errors.Is(err, core.ErrBadMarkup) {
			t.Errorf("%q прошло: %v", bad, err)
		}
	}
}

// Что бы ни пришло, проверка не падает, а принятый текст не содержит ничего, кроме разрешённых тегов.
func FuzzCheckMarkup(f *testing.F) {
	for _, s := range []string{"", "<b>x</b>", "<script>", `<a href="https://x">y</a>`, "&#106;", "<<>>", "<a href='javascript:1'>", "</", "<b", "&", "<pre><code class=\"language-go\">"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			t.Skip()
		}
		if err := core.CheckMarkup(s); err != nil {
			return
		}
		low := strings.ToLower(s)
		for _, bad := range []string{"<script", "<img", "<iframe", "<svg", "<style", "<object", "<embed", "<link", "<meta", "<form"} {
			if strings.Contains(low, bad) {
				t.Fatalf("принят %q с %s", s, bad)
			}
		}
		if strings.Contains(low, "<a ") && !strings.Contains(low, "https://") && !strings.Contains(low, "tg://") {
			t.Fatalf("принят %q: ссылка без https и tg", s)
		}
	})
}

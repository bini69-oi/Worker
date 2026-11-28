package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Сроки входа и сессии.
const (
	sessionTTL  = 12 * time.Hour
	sessionIdle = time.Hour       // сессия без запросов дольше часа закрывается
	finishTTL   = 5 * time.Minute // за сколько после начала входа его надо закончить
)

// Пределы входа сверх пары «админ + IP»: тревога, когда пароль одного админа перебирают с разных адресов,
// и предел сообщений о входе, чтобы знающий пароль не засыпал админа похожими кнопками.
const (
	loginAcctAlarm  = 20
	loginAcctWindow = time.Hour
	loginMsgTries   = 5
	loginMsgWindow  = 10 * time.Minute
)

// hashSlots ограничивает число одновременных проверок пароля: каждая берёт 19 МБ памяти,
// и поток попыток с тысяч адресов не должен съесть память сервера.
var hashSlots = make(chan struct{}, 4)

// LoginStep это начатый вход. Браузер хранит номер и ключ и показывает код: админ нажимает в Telegram
// кнопку с тем же кодом. Так чужой вход с тем же браузером не спутать со своим.
type LoginStep struct {
	ConfirmID int64
	Nonce     string
	Code      string
}

// LoginInfo это откуда входят: показывается админу в сообщении о входе и пишется в журнал.
type LoginInfo struct {
	IP    string
	Agent string
}

// Session это сессия админки. В базе хранится только SHA-256 токена, сам токен знает лишь браузер.
type Session struct {
	ID        string // начало хэша токена, по нему владелец закрывает сессию
	AdminTg   int64
	IP        string
	Agent     string
	CreatedAt time.Time
	ExpiresAt time.Time
	SeenAt    time.Time // последний запрос: по нему сессия закрывается при простое
	ClosedAt  time.Time
}

type loginPayload struct {
	IP        string `json:"ip"`
	Agent     string `json:"agent"`
	NonceHash string `json:"nonce_hash"`
	Code      string `json:"code"`
}

// loginCodes делает код для сверки и три разных варианта кнопок, среди которых он один.
func loginCodes() (string, []string) {
	b := make([]byte, 2)
	_, _ = rand.Read(b) // rand.Read в Go не возвращает ошибку
	base := int(b[0]) % 90
	codes := make([]string, 3)
	for i := range codes {
		codes[i] = strconv.Itoa(10 + (base+i*30)%90)
	}
	return codes[int(b[1])%3], codes
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // rand.Read в Go не возвращает ошибку
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// cleanInfo обрезает IP и браузер и убирает управляющие символы: эти строки приходят от клиента
// и попадают в сообщение админу и в журнал.
func cleanInfo(in LoginInfo) LoginInfo {
	clean := func(s string, n int) string {
		r := []rune(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, s))
		return string(r[:min(len(r), n)])
	}
	return LoginInfo{IP: clean(in.IP, 64), Agent: clean(in.Agent, 200)}
}

// dummyHash нужен, чтобы вход незнакомого админа считал хэш так же долго, как знакомого,
// и по времени ответа нельзя было подобрать номера админов.
var dummyHash = sync.OnceValue(func() string { return hashPassword(randomToken()) })

// LoginStart это первый шаг входа: пароль. При верном пароле админу в Telegram уходит «Вход с IP, браузер»
// с тремя кнопками-кодами и кнопкой «Нет». Возвращает номер подтверждения, одноразовый ключ браузера
// (без него закончить вход нельзя, даже зная номер) и код, который браузер показывает админу.
// Неверный логин и неверный пароль дают одну и ту же ошибку.
//
// Попытка засчитывается до проверки пароля и своей транзакцией, поэтому пачка параллельных запросов
// счётчик не обходит. Блокируется пара «админ + IP» и сам IP: чужой не может заблокировать вход владельцу,
// а подбор пароля без кнопки в Telegram всё равно ничего не даёт. Перебор одного админа с разных адресов
// не блокируется (иначе любой закрыл бы вход владельцу), но владелец получает тревогу.
//
// IP берёт вызывающий: только из адреса соединения или от своего прокси, никогда из заголовка клиента.
func (c *Core) LoginStart(ctx context.Context, tgID int64, password string, info LoginInfo) (LoginStep, error) {
	info = cleanInfo(info)
	if info.IP == "" {
		return LoginStep{}, ErrInvalid // без адреса все входы делили бы один счётчик и блокировали друг друга
	}
	now := c.clock.Now()
	keys := []string{"login:" + itoa(tgID) + "@" + info.IP, "loginip:" + info.IP}
	var a Admin
	known, alarm, spread := false, false, false
	err := c.tx(ctx, func(tx Tx) error {
		for _, k := range keys {
			count, until, err := tx.Limit(k)
			if err != nil {
				return err
			}
			if count >= loginTries && now.Before(until) {
				return ErrLoginLocked
			}
			count++
			if err := tx.SetLimit(k, count, loginLock(count, now)); err != nil {
				return err
			}
			alarm = alarm || count == loginTries
		}
		acct := "loginacct:" + itoa(tgID)
		count, until, err := tx.Limit(acct)
		if err != nil {
			return err
		}
		if !now.Before(until) {
			count, until = 0, now.Add(loginAcctWindow)
		}
		count++
		spread = count == loginAcctAlarm
		if err := tx.SetLimit(acct, count, until); err != nil {
			return err
		}
		a, err = c.admin(tx, tgID)
		known = err == nil
		return nil
	})
	if err != nil {
		return LoginStep{}, err
	}
	if spread {
		_ = c.tx(ctx, func(tx Tx) error {
			return c.alert(tx, "admin.login_spread", map[string]string{"admin": itoa(tgID), "count": itoa(loginAcctAlarm)}, now)
		})
	}
	if known && a.Owner && a.PassHash == "" {
		c.log.Warn("пароль владельца не задан, выполните на сервере: worker admin password")
		return LoginStep{}, ErrNoPassword
	}

	// Медленный хэш считается вне транзакции записи: иначе на это время встали бы все записи, включая оплаты.
	hash := a.PassHash
	if !known {
		hash = dummyHash()
	}
	if !slowCheck(ctx, hash, password) || !known {
		if alarm {
			_ = c.tx(ctx, func(tx Tx) error {
				return c.alert(tx, "admin.login_bruteforce", map[string]string{"admin": itoa(tgID), "ip": info.IP}, now)
			})
		}
		return LoginStep{}, ErrBadLogin
	}

	step := LoginStep{Nonce: randomToken()}
	code, options := loginCodes()
	step.Code = code
	err = c.tx(ctx, func(tx Tx) error {
		if err := countTry(tx, "loginmsg:"+itoa(tgID), loginMsgTries, loginMsgWindow, now); err != nil {
			return err
		}
		for _, k := range keys {
			if err := tx.SetLimit(k, 0, now); err != nil {
				return err
			}
		}
		body, _ := json.Marshal(loginPayload{IP: info.IP, Agent: info.Agent, NonceHash: hex.EncodeToString(sha(step.Nonce)), Code: code})
		cf, err := c.newConfirm(tx, tgID, "login", body,
			map[string]string{"ip": info.IP, "agent": info.Agent, "codes": strings.Join(options, ",")}, now)
		step.ConfirmID = cf.ID
		return err
	})
	if err != nil {
		return LoginStep{}, err
	}
	return step, nil
}

// slowCheck проверяет пароль, занимая одно из мест в hashSlots.
func slowCheck(ctx context.Context, hash, password string) bool {
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		return false
	}
	defer func() { <-hashSlots }()
	return checkPassword(hash, password)
}

// LoginFinish это второй шаг: админ нажал «Да». Браузер предъявляет номер подтверждения и свой ключ
// и получает токен сессии на 12 часов. Подтверждение одноразовое.
func (c *Core) LoginFinish(ctx context.Context, confirmID int64, nonce string, info LoginInfo) (string, Session, error) {
	info = cleanInfo(info)
	token := randomToken()
	var s Session
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		cf, err := tx.Confirm(confirmID)
		if err != nil || cf.Kind != "login" || now.Sub(cf.CreatedAt) > finishTTL {
			return ErrNotConfirmed
		}
		var p loginPayload
		if err := json.Unmarshal(cf.Payload, &p); err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sha(nonce))), []byte(p.NonceHash)) != 1 {
			return ErrNotConfirmed
		}
		if cf.Answer != "yes" {
			return ErrNotConfirmed
		}
		if ok, err := tx.UseConfirm(confirmID, now); err != nil || !ok {
			if err == nil {
				err = ErrConfirmGone
			}
			return err
		}
		hash := sha(token)
		s = Session{ID: hex.EncodeToString(hash[:8]), AdminTg: cf.AdminTg, IP: info.IP, Agent: info.Agent,
			CreatedAt: now, ExpiresAt: now.Add(sessionTTL), SeenAt: now}
		if err := tx.CreateSession(hash, s); err != nil {
			return err
		}
		if err := c.audit(WithIP(ctx, info.IP), tx, cf.AdminTg, "login", "", map[string]string{"agent": info.Agent}, now); err != nil {
			return err
		}
		return c.alert(tx, "admin.login", map[string]string{"admin": itoa(cf.AdminTg), "ip": info.IP, "agent": info.Agent}, now)
	})
	if err != nil {
		return "", Session{}, err
	}
	return token, s, nil
}

// SessionAdmin проверяет токен сессии на каждом запросе админки и отдаёт админа с его правами сейчас.
// Сессия без запросов дольше часа закрыта. Запрос с другого IP пропускается (у телефона адрес меняется),
// но владелец получает тревогу: так видна украденная cookie.
func (c *Core) SessionAdmin(ctx context.Context, token string, info LoginInfo) (Admin, Session, error) {
	info = cleanInfo(info)
	// Быстрый путь: сессия живая, виделась меньше минуты назад и пришла с того же адреса. Тогда ничего писать не надо,
	// и обычный запрос страницы идёт чтением без транзакции записи.
	if a, s, ok := c.liveSession(ctx, token, info); ok {
		return a, s, nil
	}
	var a Admin
	var s Session
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		hash := sha(token)
		var err error
		s, err = tx.Session(hash)
		if err != nil || !s.ClosedAt.IsZero() || !now.Before(s.ExpiresAt) || now.Sub(s.SeenAt) > sessionIdle {
			return ErrSessionGone
		}
		if a, err = c.admin(tx, s.AdminTg); err != nil {
			return ErrSessionGone
		}
		if now.Sub(s.SeenAt) >= time.Minute {
			if err := tx.TouchSession(hash, now); err != nil {
				return err
			}
			s.SeenAt = now
		}
		if info.IP == "" || info.IP == s.IP {
			return nil
		}
		first, err := tx.AddNotice(0, "session_ip", s.ID+"@"+info.IP, now)
		if err != nil || !first {
			return err
		}
		return c.alert(tx, "admin.session_ip", map[string]string{"admin": itoa(s.AdminTg), "ip": s.IP, "new_ip": info.IP}, now)
	})
	return a, s, err
}

// Sessions отдаёт открытые сессии: владельцу все, помощнику только свои.
func (c *Core) Sessions(ctx context.Context, actorTg int64) ([]Session, error) {
	all, err := c.store.Sessions(ctx, c.clock.Now())
	if err != nil || c.isOwner(actorTg) {
		return all, err
	}
	var own []Session
	for _, s := range all {
		if s.AdminTg == actorTg {
			own = append(own, s)
		}
	}
	return own, nil
}

// CloseSession закрывает сессию. Свою может закрыть любой, чужую только владелец.
func (c *Core) CloseSession(ctx context.Context, actorTg int64, id string) error {
	list, err := c.store.Sessions(ctx, c.clock.Now())
	if err != nil {
		return err
	}
	for _, s := range list {
		if s.ID != id {
			continue
		}
		if s.AdminTg != actorTg && !c.isOwner(actorTg) {
			return ErrOwnerOnly
		}
		return c.tx(ctx, func(tx Tx) error {
			now := c.clock.Now()
			if err := tx.CloseSession(id, now); err != nil {
				return err
			}
			return c.audit(ctx, tx, actorTg, "session.close", id, map[string]string{"admin": itoa(s.AdminTg)}, now)
		})
	}
	return ErrNotFound
}

// liveSession проверяет сессию чтением. ok ложно, если надо идти длинным путём с записью: время обновить,
// адрес сменился или сессия не годится (тогда длинный путь отдаст нужную ошибку).
func (c *Core) liveSession(ctx context.Context, token string, info LoginInfo) (Admin, Session, bool) {
	now := c.clock.Now()
	s, err := c.store.AdminSession(ctx, sha(token))
	if err != nil || !s.ClosedAt.IsZero() || !now.Before(s.ExpiresAt) || now.Sub(s.SeenAt) >= time.Minute ||
		(info.IP != "" && info.IP != s.IP) {
		return Admin{}, Session{}, false
	}
	row, err := c.store.AdminByTg(ctx, s.AdminTg)
	a, err := c.resolveAdmin(row, err, s.AdminTg)
	return a, s, err == nil
}

package core

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"slices"
	"strconv"
	"time"
)

// UserLogin это запрос входа в браузере через бота. В базе только хэши id и nonce.
type UserLogin struct {
	ID          int64
	IDHash      []byte // хэш id, который знает только браузер
	NonceHash   []byte // хэш nonce, который знает только ссылка на бота
	Code        string // двузначный код, который показывает сайт
	TgID        int64  // кто открыл ссылку; 0, пока никто
	IP          string
	Agent       string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Answer      string // yes, no или пусто
	AnsweredAt  time.Time
	UsedAt      time.Time // когда браузер забрал сессию
	SessionHash []byte    // сессия, выданная по этому входу
}

// Сроки входа в браузере.
const webLoginTTL = 5 * time.Minute

// Состояния опроса входа.
const (
	LoginPending   = "pending"   // ждём нажатия кода в боте
	LoginConfirmed = "confirmed" // подтверждён, выдан токен
	LoginRejected  = "rejected"  // не тот код или «Это не я»
	LoginExpired   = "expired"   // срок вышел, запрос неизвестен или уже использован
)

// WebLoginStarted это начатый вход: браузер хранит ID и показывает Code, ссылка бота несёт Nonce.
type WebLoginStarted struct {
	ID        string
	Nonce     string
	Code      string
	ExpiresAt time.Time
}

// WebLoginPrompt это то, что бот показывает абоненту, открывшему ссылку: откуда входят и три кода.
type WebLoginPrompt struct {
	ID     int64
	IP     string
	Device Device // распознанные браузер и система: сырую строку клиента абонент не видит
	Codes  []string
}

// LoginAnswer это итог нажатия в боте. Locked: это была третья неверная попытка за час, вход в браузере для этого
// абонента закрыт на час.
type LoginAnswer struct {
	Confirmed bool
	Locked    bool
}

// Пределы неверных нажатий кода: три за час закрывают вход в браузере для абонента на час.
const (
	webFailMax    = 3
	webFailWindow = time.Hour
)

// webLocked говорит, закрыт ли абоненту вход в браузере.
func webLocked(tx Tx, tgID int64, now time.Time) (bool, error) {
	count, until, err := tx.Limit("weblock:" + itoa(tgID))
	return err == nil && count > 0 && now.Before(until), err
}

// webFail засчитывает неверный код. Третий за час включает блок на час. Отдаёт, включился ли блок сейчас.
func webFail(tx Tx, tgID int64, now time.Time) (bool, error) {
	key := "webfail:" + itoa(tgID)
	count, until, err := tx.Limit(key)
	if err != nil {
		return false, err
	}
	if !now.Before(until) {
		count, until = 0, now.Add(webFailWindow)
	}
	count++
	if err := tx.SetLimit(key, count, until); err != nil {
		return false, err
	}
	if count < webFailMax {
		return false, nil
	}
	return true, tx.SetLimit("weblock:"+itoa(tgID), 1, now.Add(webFailWindow))
}

// WebLoginResult это ответ на опрос. Токен есть только у LoginConfirmed и только в первый раз.
type WebLoginResult struct {
	State string
	Token string
}

// WebLoginStart начинает вход в браузере: запрос живёт 5 минут. IP и браузер берёт вызывающий из соединения.
func (c *Core) WebLoginStart(ctx context.Context, info LoginInfo) (WebLoginStarted, error) {
	info = cleanInfo(info)
	st := WebLoginStarted{ID: randomToken(), Nonce: randomToken()}
	st.Code, _ = loginCodes()
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		st.ExpiresAt = now.Add(webLoginTTL)
		_, err := tx.CreateUserLogin(UserLogin{IDHash: sha(st.ID), NonceHash: sha(st.Nonce), Code: st.Code,
			IP: info.IP, Agent: info.Agent, CreatedAt: now, ExpiresAt: st.ExpiresAt})
		return err
	})
	return st, err
}

// codeChoices даёт три разных двузначных кода в случайном порядке, среди них правильный.
func codeChoices(code string) []string {
	out := []string{code}
	for len(out) < 3 {
		b := make([]byte, 1)
		_, _ = rand.Read(b)
		n := strconv.Itoa(10 + int(b[0])%90)
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	for i := len(out) - 1; i > 0; i-- {
		j := int(b[i-1]) % (i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// WebLoginOpen это абонент открыл ссылку бота с nonce. Первый, кто открыл, занимает запрос: другой аккаунт
// получает ErrLoginUsed. Устаревшая и неизвестная ссылка: ErrLoginExpired. Подтверждённый запрос не открывается.
func (c *Core) WebLoginOpen(ctx context.Context, nonce string, tgID int64) (WebLoginPrompt, error) {
	var p WebLoginPrompt
	err := c.tx(ctx, func(tx Tx) error {
		l, err := tx.UserLoginByNonce(sha(nonce))
		if err != nil || !c.clock.Now().Before(l.ExpiresAt) {
			return ErrLoginExpired
		}
		if l.Answer != "" || !l.UsedAt.IsZero() || (l.TgID != 0 && l.TgID != tgID) {
			return ErrLoginUsed
		}
		if _, err := tx.UserByTg(tgID); err != nil {
			return err
		}
		if locked, err := webLocked(tx, tgID, c.clock.Now()); err != nil || locked {
			if err == nil {
				err = ErrLoginLocked
			}
			return err
		}
		l.TgID = tgID
		if err := tx.SaveUserLogin(l); err != nil {
			return err
		}
		p = WebLoginPrompt{ID: l.ID, IP: l.IP, Device: ClientDevice(l.Agent), Codes: codeChoices(l.Code)}
		return nil
	})
	return p, err
}

// WebLoginAnswer принимает нажатие кода в боте. Нажать может только тот, кто открыл ссылку, один раз.
// Верный код подтверждает вход. Неверный код и «no» (кнопка «Это не я») закрывают этот запрос сразу. Неверный код
// ещё идёт в счёт: три за час закрывают вход в браузере для абонента на час («Это не я» в счёт не идёт: так
// отвечает тот, кому прислали чужую ссылку, и наказывать его незачем).
func (c *Core) WebLoginAnswer(ctx context.Context, id, pressedBy int64, code string) (LoginAnswer, error) {
	var out LoginAnswer
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		l, err := tx.UserLogin(id)
		if err != nil {
			return ErrLoginExpired
		}
		if l.TgID == 0 || l.TgID != pressedBy {
			return ErrForbidden
		}
		if l.Answer != "" {
			return ErrLoginUsed
		}
		if !now.Before(l.ExpiresAt) {
			return ErrLoginExpired
		}
		if locked, err := webLocked(tx, pressedBy, now); err != nil || locked {
			if err == nil {
				err = ErrLoginLocked
			}
			return err
		}
		out.Confirmed = subtle.ConstantTimeCompare([]byte(code), []byte(l.Code)) == 1
		l.Answer = "no"
		if out.Confirmed {
			l.Answer = "yes"
		}
		l.AnsweredAt = now
		if err := tx.SaveUserLogin(l); err != nil {
			return err
		}
		if !out.Confirmed && code != "no" {
			out.Locked, err = webFail(tx, pressedBy, now)
		}
		return err
	})
	if err != nil {
		return LoginAnswer{}, err
	}
	return out, nil
}

// reissueWindow это время после выдачи сессии, в которое тот же браузер по тому же секрету может получить её снова:
// ответ на опрос мог оборваться по дороге, и сессия, которую браузер не увидел, пропала бы.
const reissueWindow = 30 * time.Second

// WebLoginPoll это опрос браузера. Подтверждённый вход отдаёт токен сессии браузера на 30 дней. Если ответ оборвался,
// повторный опрос в течение 30 секунд выдаёт новую сессию, а прежняя закрывается. Неизвестный id, истёкший и уже
// использованный запрос: LoginExpired, без подробностей.
func (c *Core) WebLoginPoll(ctx context.Context, id string, info LoginInfo) (WebLoginResult, error) {
	res := WebLoginResult{State: LoginExpired}
	if id == "" || len(id) > 128 {
		return res, nil
	}
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		l, err := tx.UserLoginByHash(sha(id))
		if err != nil {
			return nil
		}
		reissue := !l.UsedAt.IsZero() && l.Answer == "yes" && len(l.SessionHash) > 0 && now.Before(l.UsedAt.Add(reissueWindow))
		if !l.UsedAt.IsZero() && !reissue {
			return nil
		}
		switch {
		case l.Answer == "no":
			res.State = LoginRejected
		case l.Answer == "":
			if now.Before(l.ExpiresAt) {
				res.State = LoginPending
			}
		case reissue || now.Before(l.AnsweredAt.Add(webLoginTTL)): // «да»: забрать сессию можно ещё 5 минут
			u, err := tx.UserByTg(l.TgID)
			if err != nil {
				return err
			}
			if reissue {
				if err := tx.CloseUserSession(l.SessionHash, now); err != nil {
					return err
				}
			}
			token, _, err := c.openSessionTx(tx, u.ID, SessionWeb, info)
			if err != nil {
				return err
			}
			if !reissue {
				l.UsedAt = now
			}
			l.SessionHash = sha(token)
			if err := tx.SaveUserLogin(l); err != nil {
				return err
			}
			res = WebLoginResult{State: LoginConfirmed, Token: token}
		}
		return nil
	})
	if err != nil {
		return WebLoginResult{}, err
	}
	return res, nil
}

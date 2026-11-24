package core

import (
	"context"
	"crypto/rand"
	"errors"
	"strconv"
	"time"
)

const refAlphabet = "abcdefghijkmnpqrstuvwxyz23456789" // без похожих l, o, 0, 1

// newRefCode делает случайный код личной ссылки из 8 символов. Случайный, а не номер абонента,
// чтобы по ссылке нельзя было узнать, сколько абонентов у бота.
func newRefCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // rand.Read в Go не возвращает ошибку
	for i := range b {
		b[i] = refAlphabet[int(b[i])%len(refAlphabet)]
	}
	return string(b)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// registerTx создаёт абонента с личным реферальным кодом. Пригласивший записывается только здесь,
// при первом входе, поэтому старый абонент по чужой ссылке пригласившего не меняет.
func (c *Core) registerTx(tx Tx, tgID int64, username, lang, refCode string, now time.Time) (User, error) {
	u := User{TgID: tgID, Username: username, Lang: lang, CreatedAt: now}
	if refCode != "" {
		if r, err := tx.UserByRef(refCode); err == nil {
			u.ReferrerID = r.ID
		} else if !errors.Is(err, ErrNotFound) {
			return u, err
		}
	}
	var err error
	for range 3 { // совпадение случайного кода почти невозможно, но повторим
		u.RefCode = newRefCode()
		if u.ID, err = tx.CreateUser(u); !errors.Is(err, ErrDuplicate) {
			break
		}
		if _, terr := tx.UserByTg(tgID); terr == nil {
			return u, ErrDuplicate
		}
	}
	if err != nil {
		return u, err
	}
	if bonus := c.cfg.Int("ref.welcome_bonus"); u.ReferrerID != 0 && bonus > 0 {
		if _, err := move(tx, now, LedgerEntry{UserID: u.ID, Amount: bonus, Reason: ReasonReferral,
			RefType: "ref_welcome", RefID: u.ID}); err != nil {
			return u, err
		}
		u.Balance = bonus
	}
	return u, nil
}

// referralBonus начисляет пригласившему бонус за покупку приглашённого. За первую покупку действуют
// ref.first_percent и ref.first_fixed, за следующие ref.each_percent. Бонус уникален для каждой покупки.
func (c *Core) referralBonus(tx Tx, buyer User, buy LedgerEntry, price int64, now time.Time) error {
	if buyer.ReferrerID == 0 || price <= 0 {
		return nil
	}
	first, err := tx.AddNotice(buyer.ID, "ref_first_buy", "1", now)
	if err != nil {
		return err
	}
	var bonus int64
	if first {
		bonus = c.cfg.Int("ref.first_fixed") + price*c.cfg.Int("ref.first_percent")/100
	} else {
		bonus = price * c.cfg.Int("ref.each_percent") / 100
	}
	if bonus <= 0 {
		return nil
	}
	if _, err := move(tx, now, LedgerEntry{UserID: buyer.ReferrerID, Amount: bonus, Reason: ReasonReferral,
		RefType: "ref_buy", RefID: buy.ID}); err != nil {
		return err
	}
	return enqueueNotify(tx, NotifyTask{UserID: buyer.ReferrerID, Key: "notify.ref_bonus",
		Vars: map[string]string{"amount": itoa(bonus)}}, now)
}

// RefStats отдаёт цифры приглашений абонента: сколько пришло, сколько купили, сколько заработано.
func (c *Core) RefStats(ctx context.Context, userID int64) (RefStats, error) {
	return c.store.RefStats(ctx, userID)
}

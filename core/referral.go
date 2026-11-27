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
	u, err := createUserTx(tx, tgID, username, lang, refCode, now)
	if err != nil {
		return u, err
	}
	return u, enqueueAdmin(tx, AdminTask{Topic: TopicNew, Key: "admin.new_user", Vars: userVars(u)}, now)
}

// createUserTx заводит абонента без события «новый абонент»: импорт заводит тысячи и шлёт один итог.
func createUserTx(tx Tx, tgID int64, username, lang, refCode string, now time.Time) (User, error) {
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
	return u, err
}

// userVars это подстановки об абоненте для событий админам: бот делает из них ссылку на человека.
func userVars(u User) map[string]string {
	return map[string]string{"user_id": itoa(u.ID), "tg_id": itoa(u.TgID), "username": u.Username}
}

// welcomeBonus начисляет приглашённому бонус после его первой настоящей оплаты, а не при входе:
// иначе пачка новых аккаунтов по своей ссылке давала бы бесплатные деньги. Бонус уникален для абонента.
func (c *Core) welcomeBonus(tx Tx, userID int64, now time.Time) error {
	bonus := c.cfg.Int("ref.welcome_bonus")
	if bonus <= 0 {
		return nil
	}
	u, err := tx.User(userID)
	if err != nil || u.ReferrerID == 0 {
		return err
	}
	_, err = move(tx, now, LedgerEntry{UserID: u.ID, Amount: bonus, Reason: ReasonReferral, RefType: "ref_welcome", RefID: u.ID})
	if errors.Is(err, ErrDuplicate) {
		return nil
	}
	return err
}

// referralBonus начисляет пригласившему бонус за покупку приглашённого. За первую покупку действуют
// ref.first_percent и ref.first_fixed, за следующие ref.each_percent. Бонус уникален для каждой покупки.
// Бонус идёт только за абонента, который хоть раз платил настоящими деньгами: свои аккаунты по своей
// ссылке, купившие на подарки и промокоды, ничего не приносят.
func (c *Core) referralBonus(tx Tx, buyer User, refType string, refID, price int64, now time.Time) error {
	if buyer.ReferrerID == 0 || price <= 0 {
		return nil
	}
	if paid, err := tx.Paid(buyer.ID); err != nil || !paid {
		return err
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
		RefType: refType, RefID: refID}); err != nil {
		return err
	}
	return enqueueNotify(tx, NotifyTask{UserID: buyer.ReferrerID, Key: "notify.ref_bonus",
		Vars: map[string]string{"amount": itoa(bonus)}}, now)
}

// RefStats отдаёт цифры приглашений абонента: сколько пришло, сколько купили, сколько заработано.
func (c *Core) RefStats(ctx context.Context, userID int64) (RefStats, error) {
	return c.store.RefStats(ctx, userID)
}

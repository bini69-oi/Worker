package core

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Perm это право помощника. Ключи касс, токены и список админов в права не входят: это только владелец.
type Perm string

const (
	PermUsers     Perm = "users"     // смотреть и искать абонентов
	PermGrant     Perm = "grant"     // выдавать дни и деньги
	PermPromo     Perm = "promo"     // промокоды
	PermTariffs   Perm = "tariffs"   // тарифы
	PermBroadcast Perm = "broadcast" // рассылки
	PermStats     Perm = "stats"     // статистика
	PermSupport   Perm = "support"   // отвечать в поддержке
	PermBan       Perm = "ban"       // банить и разбанивать абонентов
)

// HelperPerms это все права, которые владелец может дать помощнику.
var HelperPerms = []Perm{PermUsers, PermGrant, PermBan, PermPromo, PermTariffs, PermBroadcast, PermStats, PermSupport}

// Admin это владелец или помощник. Владелец определяется только по OWNER_TG_ID из окружения.
type Admin struct {
	TgID       int64
	PassHash   string `json:"-"` // хэш пароля не уходит ни в админку, ни в выгрузку
	Perms      []Perm
	Owner      bool
	CreatedAt  time.Time
	DisabledAt time.Time
}

// AuditEntry это строка журнала действий админов.
type AuditEntry struct {
	AdminTg int64
	Action  string
	Target  string
	Details map[string]string
	IP      string
	At      time.Time
}

type ipKey struct{}

// WithIP кладёт IP запроса в контекст, чтобы журнал действий знал, откуда пришло действие.
func WithIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

func ipOf(ctx context.Context) string {
	ip, _ := ctx.Value(ipKey{}).(string)
	return ip
}

// isOwner говорит, владелец ли это. Владелец задаётся только в окружении, сменить его изнутри нельзя.
func (c *Core) isOwner(tgID int64) bool { return c.owner > 0 && tgID == c.owner }

// admin берёт админа. Владелец всегда админ со всеми правами, даже без строки в таблице.
func (c *Core) admin(tx Tx, tgID int64) (Admin, error) {
	a, err := tx.Admin(tgID)
	if c.isOwner(tgID) {
		a.TgID, a.Owner, a.Perms = tgID, true, HelperPerms
		return a, nil
	}
	// Помощник без единого права не админ. Так же отпадает строка прежнего владельца
	// после смены OWNER_TG_ID: у неё нет прав, и войти по старому паролю нельзя.
	if err != nil || !a.DisabledAt.IsZero() || len(a.Perms) == 0 {
		return Admin{}, ErrForbidden
	}
	return a, nil
}

// can проверяет право админа на каждый запрос. Попытку без права пишет в журнал asAdmin, а не can:
// запись внутри транзакции, которая вернула отказ, откатилась бы вместе с ней.
func (c *Core) can(ctx context.Context, tx Tx, tgID int64, p Perm) (Admin, error) {
	a, err := c.admin(tx, tgID)
	if err != nil {
		return a, err
	}
	if !a.Owner && !slices.Contains(a.Perms, p) {
		return a, ErrForbidden
	}
	return a, nil
}

// ownerOnly пропускает только владельца. Чужая попытка пишется в журнал и уходит владельцу тревогой.
func (c *Core) ownerOnly(ctx context.Context, tgID int64, action string) error {
	if c.isOwner(tgID) {
		return nil
	}
	c.deny(ctx, tgID, action)
	return ErrOwnerOnly
}

// asAdmin выполняет действие админа: право проверяется, действие делается и пишется в журнал
// в одной транзакции. Отказ пишется отдельной транзакцией и уходит владельцу тревогой.
// fn отдаёт, над чем действие (target) и подробности для журнала.
func (c *Core) asAdmin(ctx context.Context, actorTg int64, p Perm, action string,
	fn func(tx Tx, a Admin, now time.Time) (string, map[string]string, error)) error {
	denied := false
	err := c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		a, err := c.can(ctx, tx, actorTg, p)
		if err != nil {
			denied = errors.Is(err, ErrForbidden)
			return err
		}
		target, details, err := fn(tx, a, now)
		if err != nil {
			return err
		}
		return c.audit(ctx, tx, actorTg, action, target, details, now)
	})
	if denied {
		c.deny(ctx, actorTg, action)
	}
	return err
}

// deny пишет отказ в журнал и шлёт владельцу тревогу.
func (c *Core) deny(ctx context.Context, actorTg int64, action string) {
	now := c.clock.Now()
	_ = c.tx(ctx, func(tx Tx) error {
		if err := c.audit(ctx, tx, actorTg, "denied:"+action, "", nil, now); err != nil {
			return err
		}
		return c.alert(tx, "admin.denied", map[string]string{"admin": itoa(actorTg), "action": action}, now)
	})
}

// audit пишет действие админа в журнал. Зовётся в той же транзакции, что и само действие.
func (c *Core) audit(ctx context.Context, tx Tx, adminTg int64, action, target string, details map[string]string, now time.Time) error {
	return tx.AddAudit(AuditEntry{AdminTg: adminTg, Action: action, Target: target, Details: details, IP: ipOf(ctx), At: now})
}

// alert шлёт тревогу безопасности: в тему «Безопасность» и владельцу в личку. Выключить её нельзя,
// поэтому ни одной настройки для неё нет.
func (c *Core) alert(tx Tx, key string, vars map[string]string, now time.Time) error {
	return enqueueAdmin(tx, AdminTask{Topic: TopicSecurity, Key: key, Vars: vars}, now)
}

// Admins отдаёт список помощников. Видит его только владелец.
func (c *Core) Admins(ctx context.Context, actorTg int64) ([]Admin, error) {
	if err := c.ownerOnly(ctx, actorTg, "admins.list"); err != nil {
		return nil, err
	}
	return c.store.Admins(ctx)
}

// IsAdmin говорит, есть ли у человека право p. Бот так проверяет, кто пишет в админ-чате.
func (c *Core) IsAdmin(ctx context.Context, tgID int64, p Perm) bool {
	ok := false
	_ = c.store.Tx(ctx, func(tx Tx) error {
		_, err := c.can(ctx, tx, tgID, p)
		ok = err == nil
		return nil
	})
	return ok
}

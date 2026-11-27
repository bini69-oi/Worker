package core

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"time"
)

// PanelEntry это пользователь панели для переноса. Поля уже проверены клиентом панели.
type PanelEntry struct {
	Ref          string
	Username     string
	TgID         int64 // 0, если в панели его нет
	Status       string
	ExpireAt     time.Time
	CreatedAt    time.Time
	SubURL       string
	TrafficBytes int64
	Devices      int
	Reset        string
	Squads       []string
}

// PanelSkip это запись панели, которую клиент панели не пропустил, с причиной.
type PanelSkip struct {
	Ref, Username, Reason string
}

// PanelPage это страница списка пользователей панели.
type PanelPage struct {
	Users   []PanelEntry
	Skipped []PanelSkip
}

// ImportSkip это пропущенный при импорте пользователь панели.
type ImportSkip struct {
	Username, Reason string
}

// ImportReport это итог импорта. Он одинаковый для пробного прогона и для записи.
type ImportReport struct {
	Applied  bool
	Subs     int            // заведено подписок
	NewUsers int            // из них абонентов, которых у нас не было
	Reasons  map[string]int // причина пропуска → сколько
	Skipped  []ImportSkip   // первые importExamples пропущенных, для отчёта
}

const (
	importBatch    = 500
	importExamples = 50

	skipNoTg      = "нет Telegram ID"
	skipDisabled  = "отключён в панели"
	skipDupTg     = "Telegram ID уже у другого пользователя панели"
	skipImported  = "уже перенесён"
	skipHasSub    = "уже есть подписка"
	panelDisabled = "DISABLED"
	panelExpired  = "EXPIRED"
)

// errDryRun откатывает транзакцию пробного прогона.
var errDryRun = errors.New("пробный прогон")

// ImportPanel переносит пользователей панели в абонентов с подписками без переподключения: срок, лимиты,
// имя в панели и ссылка остаются как были. Только владелец. Без apply ничего не пишет и отдаёт тот же отчёт.
//
// Панель только читается. Пользователь с одним Telegram ID у двух записей панели заводится по самой ранней
// записи. Запись идёт транзакциями по importBatch: упавший импорт можно повторить, перенесённые пропустятся.
func (c *Core) ImportPanel(ctx context.Context, actorTg int64, apply bool) (ImportReport, error) {
	rep := ImportReport{Applied: apply, Reasons: map[string]int{}}
	if err := c.ownerOnly(ctx, actorTg, "import.panel"); err != nil {
		return rep, err
	}
	var all []PanelEntry
	err := c.panel.EachUser(ctx, func(p PanelPage) error {
		all = append(all, p.Users...)
		for _, s := range p.Skipped {
			rep.skip(s.Username, s.Reason)
		}
		return nil
	})
	if err != nil {
		return rep, err
	}
	slices.SortStableFunc(all, func(a, b PanelEntry) int { return a.CreatedAt.Compare(b.CreatedAt) })

	seen := map[int64]bool{}
	for batch := range slices.Chunk(all, importBatch) {
		var part ImportReport
		err := c.store.Tx(ctx, func(tx Tx) error {
			part = ImportReport{Reasons: map[string]int{}}
			now := c.clock.Now()
			for _, e := range batch {
				if err := c.importOne(tx, e, seen, &part, now); err != nil {
					return err
				}
			}
			if !apply {
				return errDryRun
			}
			return nil
		})
		if err != nil && !errors.Is(err, errDryRun) {
			return rep, err
		}
		rep.add(part)
	}
	if !apply {
		return rep, nil
	}
	return rep, c.tx(ctx, func(tx Tx) error {
		now := c.clock.Now()
		vars := map[string]string{"subs": strconv.Itoa(rep.Subs), "users": strconv.Itoa(rep.NewUsers), "skipped": strconv.Itoa(rep.skipped())}
		if err := c.audit(ctx, tx, actorTg, "import.panel", "", vars, now); err != nil {
			return err
		}
		return c.alert(tx, "admin.imported", vars, now)
	})
}

// importOne заводит одного пользователя панели или записывает причину пропуска. seen помнит Telegram ID,
// уже взятые в этом импорте: записи отсортированы по дате создания, поэтому выигрывает самая ранняя.
func (c *Core) importOne(tx Tx, e PanelEntry, seen map[int64]bool, rep *ImportReport, now time.Time) error {
	switch {
	case e.TgID <= 0:
		rep.skip(e.Username, skipNoTg)
		return nil
	case e.Status == panelDisabled: // включить его сверкой значило бы снять чужую блокировку
		rep.skip(e.Username, skipDisabled)
		return nil
	case seen[e.TgID]:
		rep.skip(e.Username, skipDupTg)
		return nil
	}
	seen[e.TgID] = true
	if _, err := tx.SubByPanel(e.Ref); err == nil {
		rep.skip(e.Username, skipImported)
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	u, err := tx.UserByTg(e.TgID)
	switch {
	case errors.Is(err, ErrNotFound):
		if u, err = createUserTx(tx, e.TgID, "", cmp.Or(c.cfg.String("lang.default"), "ru"), "", now); err != nil {
			return err
		}
		rep.NewUsers++
	case err != nil:
		return err
	default:
		if _, err := tx.SubByUser(u.ID); err == nil {
			rep.skip(e.Username, skipHasSub)
			return nil
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	status := SubExpired
	if e.Status != panelExpired && e.ExpireAt.After(now) {
		status = SubActive
	}
	sub := Subscription{UserID: u.ID, PanelUUID: e.Ref, SubURL: e.SubURL, ExpiresAt: e.ExpireAt, Status: status,
		CreatedAt: now, UpdatedAt: now, Imported: true,
		Own: PanelLimits{Username: e.Username, TrafficBytes: e.TrafficBytes, Devices: e.Devices, Reset: e.Reset, Squads: e.Squads}}
	if _, err := tx.SaveSub(sub); err != nil {
		return err
	}
	// Пробный период перенесённому не положен: он уже пользовался сервисом.
	if err := tx.SetTrialUsed(u.ID); err != nil {
		return err
	}
	rep.Subs++
	return nil
}

func (r *ImportReport) skip(username, reason string) {
	r.Reasons[reason]++
	if len(r.Skipped) < importExamples {
		r.Skipped = append(r.Skipped, ImportSkip{Username: username, Reason: reason})
	}
}

func (r *ImportReport) add(p ImportReport) {
	r.Subs += p.Subs
	r.NewUsers += p.NewUsers
	for reason, n := range p.Reasons {
		r.Reasons[reason] += n
	}
	for _, s := range p.Skipped {
		if len(r.Skipped) < importExamples {
			r.Skipped = append(r.Skipped, s)
		}
	}
}

func (r ImportReport) skipped() int {
	n := 0
	for _, v := range r.Reasons {
		n += v
	}
	return n
}

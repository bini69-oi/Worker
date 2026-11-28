package api

import (
	"net/http"
	"sort"
)

// AdminImportSkip это пропущенный при импорте пользователь панели.
type AdminImportSkip struct {
	Username string `json:"username"`
	Reason   string `json:"reason"`
}

// AdminImportReason это причина пропуска и сколько пользователей под неё попали.
type AdminImportReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// AdminImportReport это отчёт пробного прогона импорта: что будет заведено и кого пропустят.
type AdminImportReport struct {
	Subs     int                 `json:"subs"`      // подписок будет заведено
	NewUsers int                 `json:"new_users"` // из них новых абонентов
	Reasons  []AdminImportReason `json:"reasons"`
	Skipped  []AdminImportSkip   `json:"skipped"` // первые из пропущенных, для примера
}

func (a *adminAPI) importPreview(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	rep, err := a.d.Core.PreviewImport(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	out := AdminImportReport{Subs: rep.Subs, NewUsers: rep.NewUsers, Reasons: []AdminImportReason{}, Skipped: []AdminImportSkip{}}
	for reason, n := range rep.Reasons {
		out.Reasons = append(out.Reasons, AdminImportReason{Reason: reason, Count: n})
	}
	sort.Slice(out.Reasons, func(i, j int) bool { return out.Reasons[i].Reason < out.Reasons[j].Reason })
	for _, s := range rep.Skipped {
		out.Skipped = append(out.Skipped, AdminImportSkip{Username: s.Username, Reason: s.Reason})
	}
	return out, nil
}

// importApply просит владельца нажать «Да» в Telegram: запись идёт только после него.
func (a *adminAPI) importApply(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := a.d.Core.RequestImport(r.Context(), ad.Admin.TgID)
	return AdminSettingResult{Pending: true, ConfirmID: id}, err
}

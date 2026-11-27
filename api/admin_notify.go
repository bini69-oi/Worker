package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bini69-oi/worker/core"
)

// AdminTopic это тема админ-группы. Locked значит, что выключателя нет (тема «Безопасность»).
type AdminTopic struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
	Locked  bool   `json:"locked"`
}

// AdminNotify это раздел уведомлений: привязана ли группа и какие темы включены.
type AdminNotify struct {
	Bound  bool         `json:"bound"`
	Topics []AdminTopic `json:"topics"`
}

// AdminChatCode это одноразовый код привязки группы.
type AdminChatCode struct {
	Code string `json:"code"`
}

func (a *adminAPI) notifyGet(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	list, err := a.d.Core.SettingList(r.Context(), ad.Admin.TgID)
	if err != nil {
		return nil, err
	}
	on := map[string]bool{}
	for _, s := range list {
		if t, ok := strings.CutPrefix(s.Key, "notify."); ok {
			on[t] = s.Value == "true"
		}
	}
	out := AdminNotify{Topics: make([]AdminTopic, 0, len(core.Topics))}
	if _, err := a.d.Core.AdminChat(r.Context()); err == nil {
		out.Bound = true
	} else if !errors.Is(err, core.ErrNotFound) {
		return nil, err
	}
	for _, id := range core.Topics {
		locked := id == core.TopicSecurity
		out.Topics = append(out.Topics, AdminTopic{ID: id, Title: a.s.deps.Text("topic."+id, ""), Enabled: locked || on[id], Locked: locked})
	}
	return out, nil
}

func (a *adminAPI) notifyCode(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	code, err := a.d.Core.NewChatCode(r.Context(), ad.Admin.TgID)
	return AdminChatCode{Code: code}, err
}

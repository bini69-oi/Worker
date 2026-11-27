package api

import (
	"net/http"
	"time"

	"github.com/bini69-oi/worker/core"
)

// AdminBroadcast это рассылка со счётчиками: страница опрашивает её, пока идёт отправка.
type AdminBroadcast struct {
	ID         int64      `json:"id"`
	Segment    string     `json:"segment"`
	Text       string     `json:"text"`
	ButtonText string     `json:"button_text"`
	ButtonURL  string     `json:"button_url"`
	Status     string     `json:"status"` // draft, running, done, cancelled
	Total      int        `json:"total"`
	Sent       int        `json:"sent"`
	Failed     int        `json:"failed"`
	Blocked    int        `json:"blocked"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	DoneAt     *time.Time `json:"done_at"`
}

// AdminBroadcastList это страница рассылок.
type AdminBroadcastList struct {
	Items []AdminBroadcast `json:"items"`
	Next  int64            `json:"next"`
}

// AdminSegmentCount это число получателей сегмента.
type AdminSegmentCount struct {
	Count int `json:"count"`
}

func broadcastDTO(b core.Broadcast) AdminBroadcast {
	return AdminBroadcast{ID: b.ID, Segment: b.Segment, Text: b.Text, ButtonText: b.ButtonText, ButtonURL: b.ButtonURL,
		Status: b.Status, Total: b.Total, Sent: b.Sent, Failed: b.Failed, Blocked: b.Blocked, CreatedAt: b.CreatedAt,
		StartedAt: optTime(b.StartedAt), DoneAt: optTime(b.DoneAt)}
}

func (a *adminAPI) broadcastsList(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	cur, err := queryCursor(r)
	if err != nil {
		return nil, err
	}
	page, err := a.d.Core.Broadcasts(r.Context(), ad.Admin.TgID, cur)
	if err != nil {
		return nil, err
	}
	out := AdminBroadcastList{Items: make([]AdminBroadcast, 0, len(page.Items)), Next: page.Next}
	for _, b := range page.Items {
		out.Items = append(out.Items, broadcastDTO(b))
	}
	return out, nil
}

func (a *adminAPI) broadcastGet(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	b, err := a.d.Core.BroadcastByID(r.Context(), ad.Admin.TgID, id)
	return broadcastDTO(b), err
}

func (a *adminAPI) broadcastSegment(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	n, err := a.d.Core.CountSegment(r.Context(), ad.Admin.TgID, r.URL.Query().Get("segment"))
	return AdminSegmentCount{Count: n}, err
}

func (a *adminAPI) broadcastCreate(w http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	var in struct {
		Segment    string `json:"segment"`
		Text       string `json:"text"`
		ButtonText string `json:"button_text"`
		ButtonURL  string `json:"button_url"`
	}
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	b, err := a.d.Core.CreateBroadcast(r.Context(), ad.Admin.TgID,
		core.Broadcast{Segment: in.Segment, Text: in.Text, ButtonText: in.ButtonText, ButtonURL: in.ButtonURL})
	if err != nil {
		return nil, err
	}
	return broadcastDTO(b), nil
}

func (a *adminAPI) broadcastTest(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return nil, a.d.Core.TestBroadcast(r.Context(), ad.Admin.TgID, id)
}

func (a *adminAPI) broadcastStart(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	b, err := a.d.Core.StartBroadcast(r.Context(), ad.Admin.TgID, id)
	return broadcastDTO(b), err
}

func (a *adminAPI) broadcastCancel(_ http.ResponseWriter, r *http.Request, ad adminCtx) (any, error) {
	id, err := pathID(r)
	if err != nil {
		return nil, err
	}
	return nil, a.d.Core.CancelBroadcast(r.Context(), ad.Admin.TgID, id)
}

package paneltest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (p *Panel) listDevices(w http.ResponseWriter, ref string) {
	u := p.find(ref)
	if u == nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	list := make([]map[string]any, 0, len(u.Devices))
	for _, h := range u.Devices {
		list = append(list, map[string]any{
			"hwid": h, "platform": "iOS", "deviceModel": "iPhone",
			"createdAt": "2026-01-01T00:00:00.000Z",
		})
	}
	reply(w, map[string]any{"devices": list, "total": len(list)})
}

// deleteDevice принимает userUuid в 2.x и числовой userId в 3.x. Без hwid удаляет все устройства (delete-all).
func (p *Panel) deleteDevice(w http.ResponseWriter, r *http.Request) {
	var b struct {
		UserUUID string `json:"userUuid"`
		UserID   int64  `json:"userId"`
		HWID     string `json:"hwid"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	ref := b.UserUUID
	if p.gen == V3 {
		ref = itoa(b.UserID)
	}
	u := p.find(ref)
	if u == nil {
		fail(w, http.StatusNotFound, "user not found")
		return
	}
	if b.HWID == "" {
		if !strings.HasSuffix(r.URL.Path, "/delete-all") {
			fail(w, http.StatusBadRequest, "hwid required")
			return
		}
		u.Devices = nil
		reply(w, map[string]any{"total": 0, "devices": []any{}})
		return
	}
	kept := u.Devices[:0]
	for _, h := range u.Devices {
		if h != b.HWID {
			kept = append(kept, h)
		}
	}
	u.Devices = kept
	reply(w, map[string]any{"ok": true})
}

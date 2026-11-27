package api

import (
	"net/http"
	"strings"

	"github.com/bini69-oi/worker/apps"
	"github.com/bini69-oi/worker/core"
)

func (a *App) connectRoutes() {
	a.handle("GET /api/app/connect", a.connect)
	a.handle("GET /api/app/devices", a.devices)
	a.handle("DELETE /api/app/devices/{hwid}", a.deleteDevice)
	a.handle("POST /api/app/devices/reset", a.resetDevices)
	a.handle("POST /api/app/link/revoke", a.revokeLink)
}

// maxHWID это предел длины идентификатора устройства из адреса.
const maxHWID = 128

// connect отдаёт приложения и ссылку подписки. Переход /dl подписан ключом и живёт сутки, как в боте.
func (a *App) connect(_ http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	sub, err := a.Core.Sub(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	out := ConnectResponse{SubURL: sub.SubURL, Apps: []AppView{}}
	if sub.SubURL == "" {
		return out, nil
	}
	tok := apps.Token(a.DLKey, au.User.ID, sub.SubURL, a.now())
	for _, ap := range apps.List(a.Config.String("connect.apps")) {
		v := AppView{ID: ap.ID, Name: ap.Name, Stores: map[string]string{}, OpenURL: strings.TrimSuffix(a.PublicURL, "/") + "/dl/" + ap.ID + "/" + tok}
		for _, p := range apps.Platforms {
			if u, ok := ap.Stores[p]; ok {
				v.Stores[string(p)] = u
			}
		}
		out.Apps = append(out.Apps, v)
	}
	return out, nil
}

func (a *App) devices(_ http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	list, err := a.Core.Devices(r.Context(), au.User.ID)
	if err != nil {
		return nil, err
	}
	out := DevicesResponse{Devices: []DeviceView{}}
	for _, d := range list {
		out.Devices = append(out.Devices, DeviceView{HWID: d.HWID, Platform: d.Platform, Model: d.Model, CreatedAt: d.CreatedAt})
	}
	return out, nil
}

func (a *App) deleteDevice(_ http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	hwid := r.PathValue("hwid")
	if hwid == "" || len(hwid) > maxHWID {
		return nil, core.ErrInvalid
	}
	return nil, a.Core.DeleteDevice(r.Context(), au.User.ID, hwid)
}

func (a *App) resetDevices(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in EmptyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	return nil, a.Core.ResetDevices(r.Context(), au.User.ID)
}

// revokeLink перевыпускает ссылку подписки: старая перестаёт работать.
func (a *App) revokeLink(w http.ResponseWriter, r *http.Request, au Auth) (any, error) {
	var in EmptyRequest
	if err := Decode(w, r, &in); err != nil {
		return nil, err
	}
	if _, err := a.Core.RevokeLink(r.Context(), au.User.ID); err != nil {
		return nil, err
	}
	return a.connect(w, r, au)
}

package paneltest

import (
	"net/http"
	"strings"
)

// match превращает запрос в имя маршрута и его параметры.
// Имя годится для Panel.Calls, например "GET /api/users/{ref}".
func match(r *http.Request) (string, []string) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	key := func(path string, args ...string) (string, []string) {
		return r.Method + " " + path, args
	}
	n := len(parts)
	switch {
	case n == 3 && parts[1] == "system":
		return key("/api/system/" + parts[2])
	case n == 2 && parts[1] == "users":
		return key("/api/users")
	case n == 4 && parts[1] == "users" && parts[2] == "by-username":
		return key("/api/users/by-username/{username}", parts[3])
	case n == 3 && parts[1] == "users":
		return key("/api/users/{ref}", parts[2])
	case n == 5 && parts[1] == "users" && parts[3] == "actions":
		return key("/api/users/{ref}/actions/{action}", parts[2], parts[4])
	case n == 4 && parts[1] == "hwid" && parts[2] == "devices" && parts[3] == "delete":
		return key("/api/hwid/devices/delete")
	case n == 4 && parts[1] == "hwid" && parts[2] == "devices" && parts[3] == "delete-all":
		return key("/api/hwid/devices/delete-all")
	case n == 4 && parts[1] == "hwid" && parts[2] == "devices":
		return key("/api/hwid/devices/{ref}", parts[3])
	}
	return key(r.URL.Path)
}

// route раздаёт запрос по маршруту. Вызывается с захваченным замком.
func (p *Panel) route(w http.ResponseWriter, r *http.Request, route string, args []string) {
	switch route {
	case "GET /api/system/metadata", "GET /api/system/health":
		p.version(w)
	case "POST /api/users":
		p.createUser(w, r)
	case "PATCH /api/users":
		p.patchUser(w, r)
	case "GET /api/users/by-username/{username}":
		p.byUsername(w, args[0])
	case "GET /api/users/{ref}":
		p.getUser(w, args[0])
	case "POST /api/users/{ref}/actions/{action}":
		p.userAction(w, args[0], args[1])
	case "GET /api/hwid/devices/{ref}":
		p.listDevices(w, args[0])
	case "POST /api/hwid/devices/delete":
		p.deleteDevice(w, r)
	case "POST /api/hwid/devices/delete-all":
		p.deleteDevice(w, r)
	default:
		fail(w, http.StatusNotFound, "no such route")
	}
}

func (p *Panel) version(w http.ResponseWriter) {
	v := "2.8.4"
	if p.gen == V3 {
		v = "3.4.1"
	}
	reply(w, map[string]any{"version": v})
}

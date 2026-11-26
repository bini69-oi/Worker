// Package apps это приложения для подключения к VPN и переход в них по кнопке. Его делят бот и кабинет:
// список приложений и переход одни, поэтому не расходятся.
package apps

import (
	"fmt"
	"strings"

	"github.com/bini69-oi/worker/settings"
)

// Platform это платформа, под которую есть ссылка на магазин.
type Platform string

const (
	IOS     Platform = "ios"
	Android Platform = "android"
	Desktop Platform = "desktop"
)

// Platforms это платформы в порядке показа.
var Platforms = []Platform{IOS, Android, Desktop}

// App это приложение: как открыть его со ссылкой подписки и где скачать.
type App struct {
	ID     string
	Name   string
	Scheme string // ссылка открытия, {url} заменяется ссылкой подписки
	Stores map[Platform]string
}

// Open отдаёт ссылку, которая открывает приложение и добавляет в него подписку.
func (a App) Open(subURL string) string { return strings.ReplaceAll(a.Scheme, "{url}", subURL) }

// all это известные приложения. Новое приложение добавляется сюда одной строкой.
var all = []App{
	{ID: "happ", Name: "Happ", Scheme: "happ://add/{url}", Stores: map[Platform]string{
		IOS:     "https://apps.apple.com/app/happ-proxy-utility/id6504287215",
		Android: "https://play.google.com/store/apps/details?id=com.happproxy",
		Desktop: "https://github.com/Happ-proxy/happ-desktop/releases/latest",
	}},
	{ID: "v2raytun", Name: "v2RayTun", Scheme: "v2raytun://import/{url}", Stores: map[Platform]string{
		IOS:     "https://apps.apple.com/app/v2raytun/id6476628951",
		Android: "https://play.google.com/store/apps/details?id=com.v2raytun.android",
	}},
	{ID: "hiddify", Name: "Hiddify", Scheme: "hiddify://import/{url}", Stores: map[Platform]string{
		IOS:     "https://apps.apple.com/app/hiddify-proxy-vpn/id6596777532",
		Android: "https://play.google.com/store/apps/details?id=app.hiddify.com",
		Desktop: "https://github.com/hiddify/hiddify-app/releases/latest",
	}},
	{ID: "streisand", Name: "Streisand", Scheme: "streisand://import/{url}", Stores: map[Platform]string{
		IOS: "https://apps.apple.com/app/streisand/id6450534064",
	}},
}

func init() { settings.SetCheck("connect.apps", Check) }

// Find отдаёт приложение по id.
func Find(id string) (App, bool) {
	for _, a := range all {
		if a.ID == id {
			return a, true
		}
	}
	return App{}, false
}

// List отдаёт приложения из настройки connect.apps в её порядке. Неизвестные id пропускаются.
func List(setting string) []App {
	var out []App
	for _, id := range strings.Split(setting, ",") {
		if a, ok := Find(strings.TrimSpace(id)); ok {
			out = append(out, a)
		}
	}
	return out
}

// Check проверяет настройку connect.apps: хотя бы одно приложение, все id известны.
func Check(setting string) error {
	var ids []string
	for _, a := range all {
		ids = append(ids, a.ID)
	}
	n := 0
	for _, id := range strings.Split(setting, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := Find(id); !ok {
			return fmt.Errorf("неизвестное приложение %q, можно: %s", id, strings.Join(ids, ", "))
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("нужно хотя бы одно приложение: %s", strings.Join(ids, ", "))
	}
	return nil
}

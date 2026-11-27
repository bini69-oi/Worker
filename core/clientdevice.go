package core

import "strings"

// Device это браузер и система из строки User-Agent. Только слова из короткого белого списка: строку клиент задаёт
// сам, и показывать её абоненту как есть значит дать злоумышленнику место для текста в сообщении бота.
// Не узнали: пустое поле.
type Device struct {
	Browser string
	OS      string
}

// browsers идут по порядку проверки: Chrome есть в строке почти у всех, поэтому он последний из семейства.
var browsers = []struct{ mark, name string }{
	{"Edg/", "Edge"}, {"EdgA/", "Edge"}, {"EdgiOS/", "Edge"},
	{"OPR/", "Opera"}, {"YaBrowser", "Yandex Browser"}, {"SamsungBrowser", "Samsung Internet"},
	{"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"},
	{"Chrome/", "Chrome"}, {"CriOS/", "Chrome"},
	{"Safari/", "Safari"},
}

// ClientDevice узнаёт браузер и систему по User-Agent.
func ClientDevice(ua string) Device {
	var d Device
	for _, b := range browsers {
		if strings.Contains(ua, b.mark) {
			d.Browser = b.name
			break
		}
	}
	switch {
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "iPod"):
		d.OS = "iOS"
	case strings.Contains(ua, "Android"):
		d.OS = "Android"
	case strings.Contains(ua, "Windows NT"):
		d.OS = "Windows"
	case strings.Contains(ua, "CrOS"):
		d.OS = "ChromeOS"
	case strings.Contains(ua, "Mac OS X"), strings.Contains(ua, "Macintosh"):
		d.OS = "macOS"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		d.OS = "Linux"
	}
	return d
}

// String собирает «Chrome, Windows». Пусто, если не узнали ничего.
func (d Device) String() string {
	switch {
	case d.Browser != "" && d.OS != "":
		return d.Browser + ", " + d.OS
	case d.Browser != "":
		return d.Browser
	}
	return d.OS
}

package guard

import (
	"net/http"
	"net/netip"
	"strings"
)

// Allowed говорит, пускает ли список адресов клиента. Список это IP и сети через запятую; пустой список пускает
// всех. Неразобранная запись не пускает никого: настройка проверяется при сохранении, а повреждённая
// не должна открывать закрытое. ip это настоящий адрес клиента (после RealIP).
func Allowed(list, ip string) bool {
	if strings.TrimSpace(list) == "" {
		return true
	}
	addr, err := netip.ParseAddr(Host(ip))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if p, err := netip.ParsePrefix(item); err == nil {
			if p.Masked().Contains(addr) {
				return true
			}
			continue
		}
		if a, err := netip.ParseAddr(item); err == nil && a.Unmap() == addr {
			return true
		}
	}
	return false
}

// OnlyFrom пускает к next только адреса из списка, который отдаёт list при каждом запросе (правка настройки
// действует сразу). Чужой адрес получает 404, как будто страницы нет: сам факт админки не светится.
func OnlyFrom(list func() string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Allowed(list(), r.RemoteAddr) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

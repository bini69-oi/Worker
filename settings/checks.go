package settings

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

func init() {
	SetCheck("addons.traffic", checkTrafficPacks)
	SetCheck("admin.allow_ips", checkAllowIPs)
}

const (
	maxPacks    = 10
	maxPackGB   = 10000
	maxAllowIPs = 50
)

// checkTrafficPacks проверяет пакеты докупки трафика: «гигабайты:цена в копейках» через запятую. Пусто это «докупки нет».
// Строка с опечаткой не молчит и не пропускается: админ видит пример формата.
func checkTrafficPacks(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	bad := errors.New("формат пакета: гигабайты:цена в копейках, пакеты через запятую, пример 10:15000,50:50000")
	seen := map[int64]bool{}
	items := strings.Split(v, ",")
	if len(items) > maxPacks {
		return errors.New("пакетов не больше 10, пример 10:15000,50:50000")
	}
	for _, item := range items {
		gb, price, ok := strings.Cut(strings.TrimSpace(item), ":")
		g, err1 := strconv.ParseInt(gb, 10, 64)
		p, err2 := strconv.ParseInt(price, 10, 64)
		if !ok || err1 != nil || err2 != nil || g <= 0 || g > maxPackGB || p <= 0 || p > maxMoney {
			return bad
		}
		if seen[g] {
			return errors.New("пакет на " + gb + " ГБ повторяется, пример 10:15000,50:50000")
		}
		seen[g] = true
	}
	return nil
}

// checkAllowIPs проверяет список адресов админки: IP или сети через запятую. Опечатка в списке заперла бы владельца
// или открыла лишнее, поэтому неразобранное не принимается.
func checkAllowIPs(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	items := strings.Split(v, ",")
	if len(items) > maxAllowIPs {
		return errors.New("адресов не больше 50")
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if _, err := netip.ParsePrefix(item); err == nil {
			continue
		}
		if _, err := netip.ParseAddr(item); err != nil {
			return errors.New("не адрес и не сеть: пример 203.0.113.5, 10.0.0.0/8")
		}
	}
	return nil
}

package bot

import (
	"errors"
	"strconv"
	"strings"
)

// maxData это предел данных кнопки в Telegram.
const maxData = 64

var errBadData = errors.New("данные кнопки не разобрать")

// Data это разобранная кнопка: действие и аргументы. Вид в Telegram: `действие:арг1:арг2`.
// Кнопка несёт только действие и id, никаких сумм и прав: всё проверяет ядро.
type Data struct {
	Act  string
	Args []string
}

// data собирает данные кнопки. Длиннее 64 байт не бывает: это ошибка в коде, тест её ловит.
func data(act string, args ...any) string {
	parts := []string{act}
	for _, a := range args {
		switch v := a.(type) {
		case string:
			parts = append(parts, v)
		case int:
			parts = append(parts, strconv.Itoa(v))
		case int64:
			parts = append(parts, strconv.FormatInt(v, 10))
		default:
			panic("bot: неизвестный тип аргумента кнопки")
		}
	}
	s := strings.Join(parts, ":")
	if len(s) > maxData {
		panic("bot: данные кнопки длиннее 64 байт: " + act)
	}
	return s
}

// parseData разбирает данные кнопки от Telegram. Это чужой ввод: проверяем длину и алфавит.
func parseData(s string) (Data, error) {
	if s == "" || len(s) > maxData {
		return Data{}, errBadData
	}
	parts := strings.Split(s, ":")
	if len(parts) > 5 || !word(parts[0], 1, 16, false) {
		return Data{}, errBadData
	}
	for _, p := range parts[1:] {
		if !word(p, 0, 32, true) {
			return Data{}, errBadData
		}
	}
	return Data{Act: parts[0], Args: parts[1:]}, nil
}

// word проверяет, что s из латиницы, цифр и `_` (и `-`, если dash), длиной от min до max.
func word(s string, min, max int, dash bool) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || dash && r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// pair отдаёт два первых аргумента числами, например тариф и срок.
func (d Data) pair() (int64, int64, bool) {
	a, ok1 := d.Int(0)
	b, ok2 := d.Int(1)
	return a, b, ok1 && ok2
}

// Int отдаёт аргумент i как положительное число. Нет аргумента или не число: false.
func (d Data) Int(i int) (int64, bool) {
	if i >= len(d.Args) {
		return 0, false
	}
	n, err := strconv.ParseInt(d.Args[i], 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

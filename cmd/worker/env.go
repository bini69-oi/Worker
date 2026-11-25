package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Env это всё, что читается из окружения. Остальные настройки живут в базе.
type Env struct {
	BotToken   string
	OwnerID    int64 // Telegram ID владельца, сменить его изнутри системы нельзя
	PublicURL  string
	PanelURL   string
	PanelToken string
	SecretKey  []byte // мастер-ключ, 32 байта, им шифруются секреты в базе
	DBPath     string
}

var requiredKeys = []string{"BOT_TOKEN", "OWNER_TG_ID", "PUBLIC_URL", "PANEL_URL", "PANEL_TOKEN", "SECRET_KEY"}

const defaultDBPath = "worker.db"

// loadEnv читает файл .env (если он есть) и переменные процесса. Переменная процесса важнее файла.
// Возвращает предупреждения, которые стоит записать в журнал. Ни ошибки, ни предупреждения не содержат значений.
func loadEnv(path string, getenv func(string) string) (Env, []string, error) {
	file, warns, err := readEnvFile(path)
	if err != nil {
		return Env{}, nil, err
	}

	get := func(key string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return file[key]
	}

	var missing []string
	for _, key := range requiredKeys {
		if get(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return Env{}, warns, fmt.Errorf("не заданы переменные: %s", strings.Join(missing, ", "))
	}

	env := Env{
		BotToken:   get("BOT_TOKEN"),
		PanelToken: get("PANEL_TOKEN"),
		DBPath:     get("DB_PATH"),
	}
	if env.DBPath == "" {
		env.DBPath = defaultDBPath
	}

	env.OwnerID, err = strconv.ParseInt(get("OWNER_TG_ID"), 10, 64)
	if err != nil || env.OwnerID <= 0 {
		return Env{}, warns, errors.New("OWNER_TG_ID: нужен числовой Telegram ID владельца")
	}
	if env.PublicURL, err = cleanURL(get("PUBLIC_URL"), true); err != nil {
		return Env{}, warns, fmt.Errorf("PUBLIC_URL: %w", err)
	}
	if env.PanelURL, err = cleanURL(get("PANEL_URL"), false); err != nil {
		return Env{}, warns, fmt.Errorf("PANEL_URL: %w", err)
	}
	env.SecretKey, err = base64.StdEncoding.DecodeString(get("SECRET_KEY"))
	if err != nil || len(env.SecretKey) != 32 {
		return Env{}, warns, errors.New("SECRET_KEY: нужно 32 байта в base64, получить: openssl rand -base64 32")
	}
	return env, warns, nil
}

// readEnvFile читает .env. Файла нет, это не ошибка: всё может прийти из переменных процесса.
func readEnvFile(path string) (map[string]string, []string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var warns []string
	if st, err := f.Stat(); err == nil && st.Mode().Perm()&0o077 != 0 {
		warns = append(warns, fmt.Sprintf("%s читают другие пользователи сервера, выполните: chmod 600 %s", path, path))
	}

	vals, err := parseEnv(f)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	for key := range vals {
		if !knownKey(key) {
			warns = append(warns, fmt.Sprintf("переменная %s не читается: настройки задаются в админке", key))
		}
	}
	return vals, warns, nil
}

// parseEnv понимает строки KEY=VALUE, комментарии через # и значения в кавычках.
func parseEnv(r io.Reader) (map[string]string, error) {
	vals := map[string]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("строка %d: нужен вид KEY=VALUE", n)
		}
		vals[key] = unquote(strings.TrimSpace(val))
	}
	return vals, sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func knownKey(key string) bool {
	return key == "DB_PATH" || slices.Contains(requiredKeys, key)
}

// cleanURL проверяет адрес и убирает слэш в конце. Публичный адрес только https: иначе Telegram не примет вебхук.
func cleanURL(raw string, httpsOnly bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("нужен полный адрес вида https://example.com")
	}
	if u.Scheme != "https" && (httpsOnly || u.Scheme != "http") {
		if httpsOnly {
			return "", errors.New("нужен адрес на https")
		}
		return "", errors.New("нужен адрес на http или https")
	}
	if u.Scheme == "http" && !inside(u.Hostname()) {
		// По http ключ панели шёл бы по сети открытым текстом, а ответы панели можно было бы подменить.
		return "", errors.New("http можно только до панели на этом сервере или в своей сети, иначе нужен https")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// inside говорит, что адрес внутренний: этот сервер, частная сеть или имя без точки (контейнер в своей сети).
func inside(host string) bool {
	if host == "localhost" || (host != "" && !strings.Contains(host, ".") && net.ParseIP(host) == nil) {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

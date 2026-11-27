package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/bini69-oi/worker/release"
)

// version задаётся при сборке релиза (-X main.version=vX.Y.Z).
var version = "dev"

// releasesURL это адрес релизов проекта на GitHub.
const releasesURL = "https://github.com/bini69-oi/worker/releases"

const (
	maxSums   = 1 << 20
	maxSig    = 1 << 10
	maxBinary = 256 << 20
)

var versionTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// updater обновляет бинарник из релиза. Всё внешнее (сеть, бэкап, перезапуск службы) подставляется,
// поэтому порядок шагов и откат проверяются тестами без сервера.
type updater struct {
	BaseURL string // адрес релизов; в тестах фейковый сервер
	Client  *http.Client
	PubKey  []byte // открытый ключ подписи релизов (PEM)
	Arch    string // amd64 или arm64
	BinPath string // путь к бинарнику, который заменяется
	Out     io.Writer
	Backup  func(ctx context.Context) (string, error)
	Restart func(ctx context.Context) error
}

// Run обновляет до версии tag ("" значит последняя). Порядок: подпись списка сумм, скачивание и сумма бинарника,
// бэкап, копия прежнего бинарника, атомарная замена, перезапуск. До бэкапа на сервере ничего не меняется.
func (u *updater) Run(ctx context.Context, tag string) error {
	if tag != "" && !versionTag.MatchString(tag) {
		return errors.New("версия задаётся как v1.2.3")
	}
	if u.Arch != "amd64" && u.Arch != "arm64" {
		return fmt.Errorf("архитектура %s не поддерживается, есть amd64 и arm64", u.Arch)
	}
	base := u.BaseURL + "/latest/download/"
	if tag != "" {
		base = u.BaseURL + "/download/" + tag + "/"
	}
	name := "worker-linux-" + u.Arch

	sums, err := u.get(ctx, base+"SHA256SUMS", maxSums)
	if err != nil {
		return err
	}
	sig, err := u.get(ctx, base+"SHA256SUMS.sig", maxSig)
	if err != nil {
		return err
	}
	if err := release.Verify(u.PubKey, sums, sig); err != nil {
		return fmt.Errorf("релиз не принят, прежняя версия работает: %w", err)
	}
	want, err := sumFor(sums, name)
	if err != nil {
		return err
	}

	newPath := u.BinPath + ".new"
	got, err := u.download(ctx, base+name, newPath)
	if err != nil {
		_ = os.Remove(newPath)
		return err
	}
	defer os.Remove(newPath)
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return errors.New("сумма скачанного файла не совпала с подписанным списком, прежняя версия работает")
	}

	cur, err := fileSum(u.BinPath)
	if err != nil {
		return err
	}
	if cur == want {
		fmt.Fprintln(u.Out, "Уже стоит эта версия, обновлять нечего.")
		return nil
	}

	backupPath, err := u.Backup(ctx)
	if err != nil {
		return fmt.Errorf("бэкап не сделан, обновление остановлено: %w", err)
	}
	fmt.Fprintf(u.Out, "Бэкап готов: %s\n", backupPath)
	return u.swap(ctx, newPath)
}

// swap кладёт новый бинарник на место прежнего: прежний копируется рядом, новый встаёт переименованием.
func (u *updater) swap(ctx context.Context, newPath string) error {
	if err := os.Chmod(newPath, 0o755); err != nil {
		return err
	}
	oldPath := u.BinPath + ".old"
	if err := copyFile(u.BinPath, oldPath); err != nil {
		return fmt.Errorf("копия прежнего бинарника не сделана, ничего не заменено: %w", err)
	}
	defer os.Remove(oldPath)
	if err := os.Rename(newPath, u.BinPath); err != nil {
		return fmt.Errorf("бинарник не заменён: %w", err)
	}
	if err := u.Restart(ctx); err != nil {
		return fmt.Errorf("служба не перезапустилась: %w", err)
	}
	fmt.Fprintln(u.Out, "Обновлено.")
	return nil
}

// get скачивает небольшой файл целиком, не больше limit байт.
func (u *updater) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := u.fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: файл больше %d байт", shortName(url), limit)
	}
	return b, nil
}

// download пишет бинарник в новый файл рядом с прежним и возвращает его SHA-256.
func (u *updater) download(ctx context.Context, url, dst string) (string, error) {
	resp, err := u.fetch(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_ = os.Remove(dst)
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBinary+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if n > maxBinary {
		return "", errors.New("бинарник больше допустимого размера")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (u *updater) fetch(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", shortName(url), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: сервер релизов ответил %d", shortName(url), resp.StatusCode)
	}
	return resp, nil
}

func shortName(url string) string { return url[strings.LastIndex(url, "/")+1:] }

// sumFor достаёт сумму файла name из списка в формате sha256sum.
func sumFor(sums []byte, name string) (string, error) {
	for _, line := range bytes.Split(sums, []byte("\n")) {
		f := strings.Fields(string(line))
		if len(f) == 2 && f[1] == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("в подписанном списке нет %s", name)
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

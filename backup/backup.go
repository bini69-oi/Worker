// Пакет backup делает зашифрованные копии базы, проверяет их и восстанавливает.
//
// Файл бэкапа это "WBK1", случайный nonce и шифротекст AES-256-GCM. Ключ выводится из SECRET_KEY через
// HMAC-SHA256 с меткой "backup", поэтому ключ, которым шифруются секреты в базе, для бэкапа не используется как есть.
// Файл читается целиком в память: база Worker это мегабайты, не гигабайты.
package backup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bini69-oi/worker/store"
)

const (
	magic      = "WBK1"
	nameLayout = "20060102-150405"
	// Interval это как часто нужен бэкап.
	Interval = 24 * time.Hour
)

// namePattern это имя файла бэкапа. По нему же считается возраст, поэтому часы файловой системы не нужны.
var namePattern = regexp.MustCompile(`^worker-(\d{8}-\d{6})\.db\.enc$`)

var errOpen = errors.New("бэкап не открылся: ключ SECRET_KEY не тот или файл повреждён")

func deriveKey(secret []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("backup"))
	return m.Sum(nil)
}

func newGCM(secret []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(secret, plain []byte) ([]byte, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(magic), nonce...)
	return gcm.Seal(out, nonce, plain, []byte(magic)), nil
}

func open(secret, data []byte) ([]byte, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return nil, err
	}
	n := len(magic) + gcm.NonceSize()
	if len(data) < n+gcm.Overhead() || string(data[:len(magic)]) != magic {
		return nil, errOpen
	}
	plain, err := gcm.Open(nil, data[len(magic):n], data[n:], []byte(magic))
	if err != nil {
		return nil, errOpen
	}
	return plain, nil
}

// Service делает бэкапы базы в папку Dir.
type Service struct {
	DB     *store.DB
	Secret []byte // SECRET_KEY
	Dir    string
	// Keep отдаёт, сколько последних бэкапов хранить (настройка backup.keep). Меньше одного считается одним.
	Keep func() int
	Now  func() time.Time
}

// Create делает бэкап и возвращает путь к файлу. Согласованная копия базы пишется во временную папку внутри Dir
// (права 700), шифруется и удаляется. Файл с таким именем уже есть: отказ, чужой бэкап не затирается.
func (s *Service) Create(ctx context.Context) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	s.sweep()
	tmp, err := os.MkdirTemp(s.Dir, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	plainPath := filepath.Join(tmp, "copy.db")
	if err := s.DB.VacuumInto(ctx, plainPath); err != nil {
		return "", fmt.Errorf("копия базы: %w", err)
	}
	plain, err := os.ReadFile(plainPath)
	if err != nil {
		return "", err
	}
	enc, err := seal(s.Secret, plain)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.Dir, "worker-"+s.Now().UTC().Format(nameLayout)+".db.enc")
	if err := writeNew(path, enc); err != nil {
		return "", err
	}
	return path, nil
}

// staleAfter это возраст, после которого временный файл считается остатком оборванного бэкапа. Моложе не трогаем:
// рядом может идти другой бэкап (задача по расписанию и ручной worker backup).
const staleAfter = 10 * time.Minute

// sweep удаляет остатки оборванных бэкапов: временные папки .tmp-* (в них копия базы без шифрования) и недописанные
// файлы *.part. Ошибки не мешают бэкапу: остаток уберёт следующий запуск.
func (s *Service) sweep() {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	cutoff := s.Now().Add(-staleAfter)
	for _, e := range entries {
		name := e.Name()
		if !(strings.HasPrefix(name, ".tmp-") || strings.HasSuffix(name, ".part")) {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(s.Dir, name))
		}
	}
}

// writeNew пишет файл целиком или не пишет вовсе: сначала рядом, потом переименование. Существующий файл не затирает.
func writeNew(path string, data []byte) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("файл %s уже есть", filepath.Base(path))
	}
	part := path + ".part"
	if err := os.WriteFile(part, data, 0o600); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Rename(part, path); err != nil {
		_ = os.Remove(part)
		return err
	}
	return nil
}

// names это имена бэкапов в папке от старых к новым.
func names(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && namePattern.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Prune удаляет всё, кроме Keep последних бэкапов. Чужие файлы в папке не трогает.
func (s *Service) Prune() error {
	all, err := names(s.Dir)
	if err != nil {
		return err
	}
	keep := max(s.Keep(), 1)
	if len(all) <= keep {
		return nil
	}
	for _, name := range all[:len(all)-keep] {
		if err := os.Remove(filepath.Join(s.Dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// RunDue делает бэкап, если последнему больше суток или его нет, и чистит старые. Звать можно часто.
func (s *Service) RunDue(ctx context.Context) error {
	s.sweep()
	all, err := names(s.Dir)
	if err != nil {
		return err
	}
	if len(all) > 0 {
		last, err := time.ParseInLocation(nameLayout, namePattern.FindStringSubmatch(all[len(all)-1])[1], time.UTC)
		if err == nil && s.Now().Sub(last) < Interval {
			return nil
		}
	}
	if _, err := s.Create(ctx); err != nil {
		return err
	}
	return s.Prune()
}

// Decrypt расшифровывает бэкап в новый файл dst (права 600). При ошибке dst не остаётся.
func Decrypt(path string, secret []byte, dst string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	plain, err := open(secret, data)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(plain)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(dst)
	}
	return werr
}

// Check расшифровывает бэкап во временную папку и проверяет целостность базы. Ничего не восстанавливает.
func Check(path string, secret []byte) error {
	tmp, err := os.MkdirTemp("", "worker-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	plain := filepath.Join(tmp, "check.db")
	if err := Decrypt(path, secret, plain); err != nil {
		return err
	}
	return store.CheckFile(plain)
}

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/bini69-oi/worker/core"
)

// maxBody это предел тела запроса в байтах. Телу кабинета и админки хватает пары строк.
const maxBody = 16 << 10

// Decode разбирает тело запроса в dst. Тело это один JSON-объект не больше maxBody без неизвестных полей.
// Больше предела: core.ErrTooLong, читать дальше предела не будет. Остальное: core.ErrInvalid.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return bodyError(err)
	}
	// После объекта допустимы только пробелы: второе значение или мусор это ошибка.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return core.ErrInvalid
		}
		return bodyError(err)
	}
	return nil
}

func bodyError(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return core.ErrTooLong
	}
	return core.ErrInvalid
}

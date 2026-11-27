// Пакет install хранит файлы установки, которые вшиваются в бинарник.
package install

import _ "embed"

// ReleasePub это открытый ключ подписи релизов (PEM). Заглушка вместо ключа даёт отказ при проверке.
//
//go:embed release.pub
var ReleasePub []byte

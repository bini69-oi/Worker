#!/usr/bin/env bash
# Установщик Worker для Debian 11+ и Ubuntu 22.04+. Запуск от root:
#
#   curl -fsSL https://raw.githubusercontent.com/bini69-oi/worker/main/install/install.sh | sudo bash
#
# Что делает: скачивает релиз, проверяет подпись и сумму ДО установки, ставит бинарник и службу systemd от
# пользователя worker. Ничего лишнего не ставит и чужие службы не трогает.
#
# Ключи:
#   --version v1.2.3   поставить эту версию вместо последней
#   --update-binary    только заменить бинарник уже установленного Worker (после смены ключа подписи)
set -euo pipefail

RELEASES_URL="${WORKER_RELEASES_URL:-https://github.com/bini69-oi/worker/releases}"
OS_RELEASE="${WORKER_OS_RELEASE:-/etc/os-release}"
BIN=/usr/local/bin/worker
ETC_DIR=/etc/worker
ENV_FILE=/etc/worker/env
DATA_DIR=/var/lib/worker
UNIT_FILE=/etc/systemd/system/worker.service
HTTP_ADDR=127.0.0.1:8080
TG_API="${WORKER_TG_API:-https://api.telegram.org}"
# Вопросы читаются из терминала, а не из stdin: при curl | bash stdin занят самим скриптом.
TTY_IN="${WORKER_INSTALL_TTY:-/dev/tty}"

# Открытый ключ подписи релизов. Должен совпадать с install/release.pub (это проверяет тест).
# BEGIN RELEASE KEY
RELEASE_KEY='# ЗАГЛУШКА. Здесь должен лежать открытый ключ подписи релизов (PEM, блок PUBLIC KEY).
# Пока ключа нет, worker update и install.sh отказываются принимать любой релиз.
# Ключ создаёт владелец командой worker sign keygen, порядок в docs/release.md.'
# END RELEASE KEY

die() {
  echo "Ошибка: $*" >&2
  exit 1
}

say() { echo "$*"; }

# machine отдаёт архитектуру процессора. Отдельной функцией, чтобы подменять в тестах.
machine() { uname -m; }

# version_at_least A B: версия A не меньше B (по правилам sort -V).
version_at_least() {
  [ -n "$1" ] && [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

require_root() {
  [ "$(id -u)" -eq 0 ] || die "запустите от root: curl ... | sudo bash"
}

check_os() {
  local id version_id
  [ -r "$OS_RELEASE" ] || die "не найден $OS_RELEASE: поддерживаются только Debian 11+ и Ubuntu 22.04+"
  # shellcheck source=/dev/null
  id=$(. "$OS_RELEASE" && echo "${ID:-}")
  # shellcheck source=/dev/null
  version_id=$(. "$OS_RELEASE" && echo "${VERSION_ID:-}")
  case "$id" in
    debian) version_at_least "$version_id" 11 || die "нужен Debian 11 или новее, у вас ${version_id:-неизвестная версия}" ;;
    ubuntu) version_at_least "$version_id" 22.04 || die "нужен Ubuntu 22.04 или новее, у вас ${version_id:-неизвестная версия}" ;;
    *) die "поддерживаются только Debian 11+ и Ubuntu 22.04+, у вас: ${id:-неизвестная система}" ;;
  esac
}

# detect_arch печатает имя архитектуры в названии релиза.
detect_arch() {
  case "$(machine)" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) die "архитектура $(machine) не поддерживается, есть amd64 и arm64" ;;
  esac
}

# check_tools проверяет, что нужные программы есть. Сам ничего не доустанавливает.
check_tools() {
  local missing=""
  for c in curl openssl systemctl useradd install sort awk; do
    command -v "$c" >/dev/null 2>&1 || missing="$missing $c"
  done
  [ -z "$missing" ] || die "не хватает программ:$missing. Установите их (apt install curl openssl) и запустите снова"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

fetch() {
  curl -fsSL --proto '=https' --tlsv1.2 --retry 2 --max-time 300 -o "$2" "$1" || die "не скачался $1"
}

# download_release DIR ARCH [VERSION] кладёт в DIR бинарник, SHA256SUMS и подпись.
download_release() {
  local dir="$1" arch="$2" version="${3:-}" base
  if [ -n "$version" ]; then
    base="$RELEASES_URL/download/$version"
  else
    base="$RELEASES_URL/latest/download"
  fi
  fetch "$base/SHA256SUMS" "$dir/SHA256SUMS"
  fetch "$base/SHA256SUMS.sig" "$dir/SHA256SUMS.sig"
  fetch "$base/worker-linux-$arch" "$dir/worker-linux-$arch"
}

# verify_ed25519 KEY MSG SIG: подпись Ed25519 сходится. OpenSSL 3 просит -rawin, OpenSSL 1.1.1 такого ключа не знает.
verify_ed25519() {
  openssl pkeyutl -verify -pubin -inkey "$1" -rawin -in "$2" -sigfile "$3" >/dev/null 2>&1 ||
    openssl pkeyutl -verify -pubin -inkey "$1" -in "$2" -sigfile "$3" >/dev/null 2>&1
}

# verify_release DIR ARCH: подпись списка сумм вшитым ключом, потом сумма бинарника. Любая неудача: выход.
verify_release() {
  local dir="$1" arch="$2" key want got
  case "$RELEASE_KEY" in
    *"BEGIN PUBLIC KEY"*) ;;
    *) die "в этом установщике нет ключа подписи релизов, установка остановлена" ;;
  esac
  key="$dir/release.pub"
  printf '%s\n' "$RELEASE_KEY" >"$key"
  verify_ed25519 "$key" "$dir/SHA256SUMS" "$dir/SHA256SUMS.sig" ||
    die "подпись релиза не сошлась, установка остановлена, на сервере ничего не изменено"
  want=$(awk -v n="worker-linux-$arch" '$2 == n {print $1}' "$dir/SHA256SUMS")
  [ -n "$want" ] || die "в подписанном списке нет worker-linux-$arch"
  got=$(sha256_of "$dir/worker-linux-$arch")
  [ "$want" = "$got" ] || die "сумма бинарника не совпала с подписанным списком, установка остановлена"
  say "Подпись и сумма проверены."
}

# install_binary кладёт бинарник атомарным переименованием.
install_binary() {
  install -m 0755 -o root -g root "$1" "$BIN.new"
  mv -f "$BIN.new" "$BIN"
}

# setup_user_dirs создаёт пользователя worker без оболочки и папки службы.
setup_user_dirs() {
  if ! id -u worker >/dev/null 2>&1; then
    useradd --system --user-group --no-create-home --home-dir "$DATA_DIR" --shell /usr/sbin/nologin worker
  fi
  install -d -o worker -g worker -m 0700 "$DATA_DIR"
  install -d -o root -g worker -m 0750 "$ETC_DIR"
}

# install_unit пишет службу. Текст совпадает с install/worker.service (это проверяет тест).
install_unit() {
  cat >"$UNIT_FILE" <<'UNIT'
[Unit]
Description=Worker: бот продажи VPN поверх Remnawave
Documentation=https://github.com/bini69-oi/worker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=worker
Group=worker
WorkingDirectory=/var/lib/worker
# Токены и ключи лежат в /etc/worker/env (права 600). Читает их systemd, процессу файл не открыт.
EnvironmentFile=/etc/worker/env
ExecStart=/usr/local/bin/worker
Restart=on-failure
RestartSec=3

# Песочница: если в боте найдут дыру, процесс не сможет писать вне своей папки, ставить программы и подняться до root.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/worker
StateDirectory=worker
StateDirectoryMode=0700
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
ProtectProc=invisible
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
RemoveIPC=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallFilter=~@privileged
SystemCallErrorNumber=EPERM
CapabilityBoundingSet=
AmbientCapabilities=
UMask=0077

[Install]
WantedBy=multi-user.target
UNIT
  chmod 0644 "$UNIT_FILE"
  systemctl daemon-reload
}

# open_input открывает терминал для вопросов на дескрипторе 3.
open_input() {
  exec 3<"$TTY_IN" || die "нет терминала для вопросов: запустите установщик в терминале (при ssh добавьте -t)"
}

# read_answer ПОДСКАЗКА [secret]: ответ в переменной ANSWER без пробелов по краям. Секрет на экране не виден.
read_answer() {
  printf '%s' "$1" >&2
  if [ -n "${2:-}" ]; then
    IFS= read -r -s -u 3 ANSWER || die "ввод оборвался"
    echo >&2
  else
    IFS= read -r -u 3 ANSWER || die "ввод оборвался"
  fi
  ANSWER="${ANSWER//$'\r'/}"
  ANSWER="${ANSWER#"${ANSWER%%[![:space:]]*}"}"
  ANSWER="${ANSWER%"${ANSWER##*[![:space:]]}"}"
}

# tg_get_me ТОКЕН: имя бота, если Telegram принял токен. Токен идёт в curl через stdin, а не в аргументах (ps его не покажет).
tg_get_me() {
  local body
  body=$(printf 'url = "%s/bot%s/getMe"\n' "$TG_API" "$1" | curl -sS --max-time 15 -K -) || return 1
  case "$body" in *'"ok":true'*) ;; *) return 1 ;; esac
  printf '%s' "$body" | sed -n 's/.*"username":"\([^"]*\)".*/\1/p'
}

# panel_status АДРЕС ТОКЕН: код ответа панели на запрос версии (000, если панель не отвечает).
panel_status() {
  printf 'url = "%s/api/system/metadata"\nheader = "Authorization: Bearer %s"\n' "$1" "$2" |
    curl -s -o /dev/null -w '%{http_code}' --max-time 15 -K - || true
}

# resolve_a ДОМЕН: IPv4-адреса из A-записи.
resolve_a() {
  getent ahostsv4 "$1" 2>/dev/null | awk '$2 == "STREAM" {print $1}' | sort -u
}

# server_ips: адреса этого сервера. Не нашлись среди интерфейсов (сервер за NAT): внешний адрес со стороннего сервиса.
server_ips() {
  if command -v ip >/dev/null 2>&1; then
    ip -4 -o addr show scope global | awk '{split($4, a, "/"); print a[1]}'
  else
    hostname -I | tr ' ' '\n' | grep -E '^[0-9.]+$' || true
  fi
}

external_ip() {
  curl -4 -fsS --max-time 10 https://api.ipify.org 2>/dev/null || true
}

check_bot_token() {
  local name
  if ! printf '%s' "$1" | grep -Eq '^[0-9]{5,}:[A-Za-z0-9_-]{20,}$'; then
    say "Это не похоже на токен бота. Он выглядит как 123456789:AAH... и выдаётся в @BotFather."
    return 1
  fi
  if ! name=$(tg_get_me "$1") || [ -z "$name" ]; then
    say "Токен не принят Telegram. Проверьте, что скопировали его целиком из @BotFather."
    return 1
  fi
  BOT_USERNAME="$name"
}

check_owner_id() {
  printf '%s' "$1" | grep -Eq '^[1-9][0-9]{0,14}$' || {
    say "Telegram ID это число, например 123456789. Узнать его можно у бота @userinfobot."
    return 1
  }
}

check_domain() {
  local ips ip mine
  if ! printf '%s' "$1" | grep -Eq '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{1,62}$'; then
    say "Нужно имя домена без https:// и без слэшей, например bot.example.com."
    return 1
  fi
  ips=$(resolve_a "$1")
  if [ -z "$ips" ]; then
    say "У домена $1 нет A-записи. Создайте её на адрес этого сервера и подождите, пока обновится DNS."
    return 1
  fi
  mine=$(server_ips)
  for ip in $ips; do
    printf '%s\n' "$mine" | grep -Fxq "$ip" && return 0
  done
  mine=$(external_ip)
  for ip in $ips; do
    [ -n "$mine" ] && [ "$ip" = "$mine" ] && return 0
  done
  say "A-запись $1 смотрит на $(printf '%s' "$ips" | tr '\n' ' '), а это не адрес сервера. Поправьте запись и подождите DNS."
  return 1
}

# inside_host ХОСТ: адрес этого сервера, своей сети или имя без точки. Туда ключ панели можно слать по http.
inside_host() {
  case "$1" in
    localhost | 127.* | 10.* | 192.168.* | 172.1[6-9].* | 172.2[0-9].* | 172.3[0-1].*) return 0 ;;
  esac
  case "$1" in *.*) return 1 ;; *) return 0 ;; esac
}

check_panel() {
  local url="$1" token="$2" host code
  if ! printf '%s' "$url" | grep -Eq '^https?://[^[:space:]"\\]+$'; then
    say "Нужен полный адрес панели, например https://panel.example.com"
    return 1
  fi
  if ! printf '%s' "$token" | grep -Eq '^[^[:space:]"\\]+$'; then
    say "В токене панели не должно быть пробелов и кавычек."
    return 1
  fi
  host="${url#*://}"
  host="${host%%/*}"
  host="${host%%:*}"
  case "$url" in
    http://*) inside_host "$host" || {
      say "По http ключ панели шёл бы по сети открытым текстом. Для панели на другом сервере нужен https."
      return 1
    } ;;
  esac
  code=$(panel_status "$url" "$token")
  case "$code" in
    200) ;;
    401 | 403)
      say "Панель отвечает, но токен не принят (код $code). Создайте API-токен в панели Remnawave."
      return 1
      ;;
    *)
      say "Панель по адресу $url не отвечает как Remnawave (код $code)."
      return 1
      ;;
  esac
}

# ask_all задаёт вопросы. Каждый ответ проверяется сразу, неверный спрашивается заново. Ничего не пишет на диск.
ask_all() {
  while :; do
    read_answer "Токен бота (из @BotFather, ввод не виден): " secret
    BOT_TOKEN="$ANSWER"
    check_bot_token "$BOT_TOKEN" && break
  done
  say "Бот найден: @$BOT_USERNAME"
  while :; do
    read_answer "Ваш Telegram ID (число): "
    OWNER_ID="$ANSWER"
    check_owner_id "$OWNER_ID" && break
  done
  while :; do
    read_answer "Домен бота (A-запись уже на этот сервер): "
    DOMAIN="${ANSWER,,}"
    check_domain "$DOMAIN" && break
  done
  while :; do
    read_answer "Адрес панели Remnawave (https://...): "
    PANEL_URL="${ANSWER%/}"
    read_answer "API-токен панели (ввод не виден): " secret
    PANEL_TOKEN="$ANSWER"
    check_panel "$PANEL_URL" "$PANEL_TOKEN" && break
  done
  SECRET_KEY=$(openssl rand -base64 32)
}

# show_key показывает ключ шифрования один раз: без него не открыть бэкапы и секреты в базе.
show_key() {
  say ""
  say "Ключ шифрования (SECRET_KEY). Сохраните его сейчас в менеджере паролей. Без него не открыть бэкапы,"
  say "и больше установщик его не покажет (он лежит только в $ENV_FILE на этом сервере):"
  say ""
  say "    $SECRET_KEY"
  say ""
  read_answer "Сохранили? Нажмите Enter, чтобы продолжить: "
}

# write_env пишет настройки службы. Файл создаётся закрытым (600), сразу под владельцем worker и меняется целиком.
write_env() {
  (
    umask 077
    local tmp
    tmp=$(mktemp "$ETC_DIR/.env.XXXXXX")
    {
      printf 'BOT_TOKEN=%s\n' "$BOT_TOKEN"
      printf 'OWNER_TG_ID=%s\n' "$OWNER_ID"
      printf 'PUBLIC_URL=https://%s\n' "$DOMAIN"
      printf 'PANEL_URL=%s\n' "$PANEL_URL"
      printf 'PANEL_TOKEN=%s\n' "$PANEL_TOKEN"
      printf 'SECRET_KEY=%s\n' "$SECRET_KEY"
      printf 'DB_PATH=%s/worker.db\n' "$DATA_DIR"
      printf 'HTTP_ADDR=%s\n' "$HTTP_ADDR"
    } >"$tmp"
    chown worker:worker "$tmp"
    chmod 600 "$tmp"
    mv -f "$tmp" "$ENV_FILE"
  )
}

health_ok() {
  curl -fsS --max-time 2 -o /dev/null "http://$HTTP_ADDR/healthz" 2>/dev/null
}

# wait_health ждёт до 60 секунд, пока служба ответит два раза подряд.
wait_health() {
  local i ok=0
  for ((i = 0; i < 60; i++)); do
    if health_ok; then
      ok=$((ok + 1))
      [ "$ok" -ge 2 ] && return 0
    else
      ok=0
    fi
    sleep 1
  done
  return 1
}

# set_owner_password задаёт пароль владельца командой worker от пользователя службы, ввод из терминала.
set_owner_password() {
  say ""
  say "Пароль владельца для админки:"
  runuser -u worker -- env WORKER_ENV_FILE="$ENV_FILE" "$BIN" admin password <&3 ||
    say "Пароль не задан. Позже: sudo -u worker env WORKER_ENV_FILE=$ENV_FILE $BIN admin password"
}

usage() {
  sed -n '2,12p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
}

main() {
  local version="" update_only=0 arch
  # tmp не local: ловушка EXIT срабатывает уже после выхода из main
  while [ $# -gt 0 ]; do
    case "$1" in
      --version)
        [ $# -ge 2 ] || die "после --version нужна версия, например v1.2.3"
        version="$2"
        shift 2
        ;;
      --update-binary)
        update_only=1
        shift
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *) die "неизвестный ключ $1" ;;
    esac
  done
  if [ -n "$version" ]; then
    printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || die "версия задаётся как v1.2.3"
  fi

  require_root
  check_os
  check_tools
  arch=$(detect_arch)

  if [ -e "$ENV_FILE" ] && [ "$update_only" -eq 0 ]; then
    say "Worker уже установлен. Обновить с бэкапом и откатом: worker update"
    say "Заменить только бинарник этим установщиком: install.sh --update-binary"
    exit 0
  fi

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  say "Скачиваю релиз ($arch)..."
  download_release "$tmp" "$arch" "$version"
  verify_release "$tmp" "$arch"

  if [ "$update_only" -eq 1 ]; then
    [ -x "$BIN" ] || die "Worker не установлен: $BIN не найден, --update-binary здесь не нужен"
    install_binary "$tmp/worker-linux-$arch"
    systemctl restart worker
    say "Бинарник заменён, служба перезапущена."
    return 0
  fi

  # Вопросы идут до установки: оборвали на любом, на сервере ничего не изменено.
  open_input
  ask_all
  show_key

  install_binary "$tmp/worker-linux-$arch"
  setup_user_dirs
  install_unit
  write_env
  systemctl enable --now worker >/dev/null 2>&1 || die "служба не запустилась: journalctl -u worker -n 50"
  say "Жду ответа службы..."
  wait_health || die "служба не ответила за 60 секунд: journalctl -u worker -n 50"
  say "Служба работает."
  set_owner_password
  say ""
  say "Готово. Бот: https://t.me/$BOT_USERNAME"
}

# WORKER_INSTALL_SOURCE_ONLY нужен тестам: подключить функции и ничего не запускать.
if [ -z "${WORKER_INSTALL_SOURCE_ONLY:-}" ]; then
  main "$@"
fi

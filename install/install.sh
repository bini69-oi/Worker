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

usage() {
  sed -n '2,12p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//' || true
}

main() {
  local version="" update_only=0 arch tmp
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

  install_binary "$tmp/worker-linux-$arch"
  if [ "$update_only" -eq 1 ]; then
    systemctl restart worker
    say "Бинарник заменён, служба перезапущена."
    return 0
  fi
  setup_user_dirs
  install_unit
  say "Бинарник и служба установлены в $BIN и $UNIT_FILE."
}

# WORKER_INSTALL_SOURCE_ONLY нужен тестам: подключить функции и ничего не запускать.
if [ -z "${WORKER_INSTALL_SOURCE_ONLY:-}" ]; then
  main "$@"
fi

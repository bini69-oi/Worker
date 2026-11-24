-- Первая схема Worker. Время везде в секундах Unix, деньги в копейках.
-- STRICT значит, что SQLite проверяет типы и не положит текст в число.

-- Абоненты
CREATE TABLE users (
    id          INTEGER PRIMARY KEY,
    tg_id       INTEGER NOT NULL UNIQUE,
    username    TEXT    NOT NULL DEFAULT '',
    lang        TEXT    NOT NULL DEFAULT 'ru',
    referrer_id INTEGER REFERENCES users (id),
    balance     INTEGER NOT NULL DEFAULT 0 CHECK (balance >= 0), -- кэш суммы ledger
    trial_used  INTEGER NOT NULL DEFAULT 0,
    banned_at   INTEGER,
    created_at  INTEGER NOT NULL
) STRICT;

-- Тарифы и их цены по срокам
CREATE TABLE tariffs (
    id            INTEGER PRIMARY KEY,
    name          TEXT    NOT NULL,
    traffic_bytes INTEGER NOT NULL DEFAULT 0 CHECK (traffic_bytes >= 0), -- 0 значит без лимита
    devices       INTEGER NOT NULL DEFAULT 0 CHECK (devices >= 0),       -- 0 значит без лимита
    reset         TEXT    NOT NULL DEFAULT 'NO_RESET',                   -- когда панель сбрасывает трафик
    squads        TEXT    NOT NULL DEFAULT '[]',                         -- JSON: группы серверов в Remnawave
    active        INTEGER NOT NULL DEFAULT 1,
    sort          INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE TABLE tariff_periods (
    tariff_id INTEGER NOT NULL REFERENCES tariffs (id) ON DELETE CASCADE,
    days      INTEGER NOT NULL CHECK (days > 0),
    price     INTEGER NOT NULL CHECK (price >= 0),
    PRIMARY KEY (tariff_id, days)
) STRICT;

-- Подписки. Цена и срок заморожены на момент покупки
CREATE TABLE subscriptions (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users (id),
    tariff_id   INTEGER REFERENCES tariffs (id), -- NULL у пробного периода
    period_days INTEGER NOT NULL,
    price       INTEGER NOT NULL,
    trial       INTEGER NOT NULL DEFAULT 0,
    panel_uuid  TEXT    NOT NULL DEFAULT '',
    sub_url     TEXT    NOT NULL DEFAULT '',
    expires_at  INTEGER NOT NULL,
    auto_renew  INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired', 'disabled')),
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX subscriptions_user ON subscriptions (user_id); -- одна подписка на абонента
CREATE INDEX subscriptions_expires ON subscriptions (status, expires_at);

-- Каждое движение денег. Сумма строк абонента равна users.balance
CREATE TABLE ledger (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id),
    amount     INTEGER NOT NULL CHECK (amount <> 0),
    reason     TEXT    NOT NULL,
    ref_type   TEXT    NOT NULL DEFAULT '',
    ref_id     INTEGER,
    admin_id   INTEGER,
    comment    TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
) STRICT;
CREATE INDEX ledger_user ON ledger (user_id, id);
-- Одно событие (платёж, возврат, бонус) двигает деньги один раз
CREATE UNIQUE INDEX ledger_ref ON ledger (ref_type, ref_id) WHERE ref_id IS NOT NULL;

-- Платежи всех касс
CREATE TABLE payments (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users (id),
    gateway      TEXT    NOT NULL,
    amount       INTEGER NOT NULL CHECK (amount > 0), -- в копейках
    ext_amount   INTEGER NOT NULL DEFAULT 0,          -- сумма в валюте кассы, например в звёздах
    ext_currency TEXT    NOT NULL DEFAULT '',
    recurring    INTEGER NOT NULL DEFAULT 0,          -- автоподписка Stars
    status       TEXT    NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'paid', 'expired', 'refunded', 'failed')),
    external_id  TEXT,
    created_at   INTEGER NOT NULL,
    paid_at      INTEGER,
    UNIQUE (gateway, external_id)
) STRICT;
CREATE INDEX payments_pending ON payments (status, created_at) WHERE status = 'pending';

-- Очередь задач к панели и уведомлений (outbox)
CREATE TABLE outbox (
    id         INTEGER PRIMARY KEY,
    kind       TEXT    NOT NULL,
    payload    TEXT    NOT NULL DEFAULT '{}',
    attempts   INTEGER NOT NULL DEFAULT 0,
    next_at    INTEGER NOT NULL,
    last_error TEXT    NOT NULL DEFAULT '',
    alerted    INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    done_at    INTEGER
) STRICT;
CREATE INDEX outbox_due ON outbox (next_at) WHERE done_at IS NULL;

-- Промокоды
CREATE TABLE promo_codes (
    id         INTEGER PRIMARY KEY,
    code       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    kind       TEXT    NOT NULL CHECK (kind IN ('money', 'days')),
    value      INTEGER NOT NULL CHECK (value > 0),
    max_uses   INTEGER NOT NULL DEFAULT 0, -- 0 значит без лимита
    uses       INTEGER NOT NULL DEFAULT 0,
    new_only   INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER,
    active     INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE promo_uses (
    promo_id INTEGER NOT NULL REFERENCES promo_codes (id),
    user_id  INTEGER NOT NULL REFERENCES users (id),
    used_at  INTEGER NOT NULL,
    PRIMARY KEY (promo_id, user_id)
) STRICT;

-- Умная корзина: что купить сразу после пополнения
CREATE TABLE pending_buys (
    user_id    INTEGER PRIMARY KEY REFERENCES users (id),
    tariff_id  INTEGER NOT NULL REFERENCES tariffs (id),
    days       INTEGER NOT NULL,
    created_at INTEGER NOT NULL
) STRICT;

-- Настройки и тексты: только отличия от умолчаний в коде
CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE texts (
    key        TEXT    NOT NULL,
    lang       TEXT    NOT NULL,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (key, lang)
) STRICT;

-- Отправленные напоминания, чтобы не слать дважды
CREATE TABLE notices (
    user_id INTEGER NOT NULL REFERENCES users (id),
    kind    TEXT    NOT NULL,
    ref     TEXT    NOT NULL,
    sent_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, kind, ref)
) STRICT;

-- Админы. Роль владельца берётся только из .env, его perms здесь не читаются
CREATE TABLE admins (
    tg_id       INTEGER PRIMARY KEY,
    pass_hash   TEXT    NOT NULL DEFAULT '',
    perms       TEXT    NOT NULL DEFAULT '[]',
    created_at  INTEGER NOT NULL,
    disabled_at INTEGER
) STRICT;

-- Сессии админки. Хранится только SHA-256 токена
CREATE TABLE admin_sessions (
    token_hash  BLOB    PRIMARY KEY,
    admin_tg_id INTEGER NOT NULL,
    ip          TEXT    NOT NULL DEFAULT '',
    agent       TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    closed_at   INTEGER
) STRICT;

-- Подтверждения кнопкой в Telegram: вход и опасные действия
CREATE TABLE confirms (
    id          INTEGER PRIMARY KEY,
    admin_tg_id INTEGER NOT NULL,
    kind        TEXT    NOT NULL,
    payload     TEXT    NOT NULL DEFAULT '{}',
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    answer      TEXT CHECK (answer IN ('yes', 'no')),
    answered_at INTEGER
) STRICT;

-- Защита от перебора пароля: ключ это админ или IP
CREATE TABLE login_fails (
    key   TEXT    PRIMARY KEY,
    count INTEGER NOT NULL DEFAULT 0,
    until INTEGER NOT NULL DEFAULT 0
) STRICT;

-- Журнал действий админов. Только вставка: правку и удаление запрещают триггеры
CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY,
    admin_tg_id INTEGER NOT NULL,
    action      TEXT    NOT NULL,
    target      TEXT    NOT NULL DEFAULT '',
    details     TEXT    NOT NULL DEFAULT '{}',
    ip          TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
) STRICT;

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log: только вставка');
END;

CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit_log: только вставка');
END;

-- Группа уведомлений админам, одна на бота
CREATE TABLE admin_chat (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    chat_id        INTEGER,
    bind_code_hash BLOB,
    topics         TEXT    NOT NULL DEFAULT '{}',
    bound_at       INTEGER
) STRICT;

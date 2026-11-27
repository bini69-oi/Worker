-- Кабинет абонента: сессии и входы в браузере через бота. Только добавляет таблицы.

-- Сессии абонента. В базе только SHA-256 токена.
CREATE TABLE user_sessions (
    token_hash BLOB    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id),
    kind       TEXT    NOT NULL,  -- tg или web
    ip         TEXT    NOT NULL DEFAULT '',
    agent      TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    seen_at    INTEGER NOT NULL,  -- последний запрос: сессия браузера без запросов закрывается
    expires_at INTEGER NOT NULL,
    closed_at  INTEGER
) STRICT;
CREATE INDEX user_sessions_user ON user_sessions (user_id) WHERE closed_at IS NULL;
CREATE INDEX user_sessions_expires ON user_sessions (expires_at); -- чистка просроченных фоновой задачей

-- Запросы входа в браузере: id знает браузер, nonce только ссылка на бота. В базе их хэши.
CREATE TABLE user_logins (
    id          INTEGER PRIMARY KEY,
    id_hash     BLOB    NOT NULL UNIQUE,
    nonce_hash  BLOB    NOT NULL UNIQUE,
    code        TEXT    NOT NULL,             -- двузначный код, который показывает сайт
    tg_id       INTEGER NOT NULL DEFAULT 0,   -- кто открыл ссылку; 0, пока никто
    ip          TEXT    NOT NULL DEFAULT '',
    agent       TEXT    NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    answer      TEXT    NOT NULL DEFAULT '',  -- yes, no или пусто
    answered_at INTEGER,
    used_at     INTEGER,                      -- когда браузер забрал сессию
    session_hash BLOB                         -- сессия, выданная по этому входу: при повторной выдаче старая закрывается
) STRICT;
CREATE INDEX user_logins_expires ON user_logins (expires_at);

-- Откуда создан платёж: пусто это бот и Mini App, web это кабинет в браузере. От этого зависит, куда вернёт касса.
ALTER TABLE payments ADD COLUMN source TEXT NOT NULL DEFAULT '';

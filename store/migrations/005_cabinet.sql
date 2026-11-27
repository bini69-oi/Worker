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
    used_at     INTEGER                       -- когда браузер забрал сессию
) STRICT;

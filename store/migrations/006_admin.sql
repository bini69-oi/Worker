-- Админка: рассылки и индексы для поиска и списков. Только добавляет, ссылок на таблицы кабинета нет.

-- Рассылка. Получателей отдельной таблицей не храним: порядок по id абонента стабилен, хватает курсора.
CREATE TABLE broadcasts (
    id             INTEGER PRIMARY KEY,
    segment        TEXT    NOT NULL CHECK (segment IN ('all', 'active', 'none', 'expired30', 'trial')),
    text           TEXT    NOT NULL,
    button_text    TEXT    NOT NULL DEFAULT '',
    button_url     TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'running', 'done', 'cancelled')),
    total          INTEGER NOT NULL DEFAULT 0,
    sent           INTEGER NOT NULL DEFAULT 0,
    failed         INTEGER NOT NULL DEFAULT 0,
    blocked        INTEGER NOT NULL DEFAULT 0,
    cursor_user_id INTEGER NOT NULL DEFAULT 0, -- до какого абонента дошли
    created_by     INTEGER NOT NULL,
    created_at     INTEGER NOT NULL,
    started_at     INTEGER,
    done_at        INTEGER
) STRICT;
-- Одна идущая рассылка за раз: вторую база не даст, даже если две попытки придут одновременно
CREATE UNIQUE INDEX broadcasts_one_running ON broadcasts (status) WHERE status = 'running';

-- Поиск по началу имени без учёта регистра
CREATE INDEX users_username ON users (username COLLATE NOCASE);
-- Список платежей и сводка идут по статусу и времени
CREATE INDEX payments_status_created ON payments (status, created_at);
CREATE INDEX payments_user ON payments (user_id, id);
-- Журнал действий: по времени и по тому, над чем действие (карточка абонента)
CREATE INDEX audit_created ON audit_log (created_at);
CREATE INDEX audit_target ON audit_log (target, id);

-- Когда платёж возвращён: выручка дня считается за вычетом возвратов этого дня
ALTER TABLE payments ADD COLUMN refunded_at INTEGER;

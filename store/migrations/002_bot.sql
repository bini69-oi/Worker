-- Бот: кто заблокировал бота, какое сообщение бот правит, кому уходит реплай поддержки.

ALTER TABLE users ADD COLUMN unreachable_at INTEGER; -- абонент заблокировал бота, уведомления не ставятся
ALTER TABLE users ADD COLUMN screen_msg_id INTEGER NOT NULL DEFAULT 0; -- сообщение-экран, которое бот правит

-- Сообщения поддержки в группе: реплай на них уходит этому абоненту. Старше 30 дней удаляются.
-- Номер сообщения уникален только внутри чата, поэтому ключ из чата и номера: после перепривязки группы
-- новые сообщения с теми же номерами не попадут к прежним абонентам.
CREATE TABLE support_msgs (
    chat_id     INTEGER NOT NULL,
    chat_msg_id INTEGER NOT NULL,
    user_id     INTEGER NOT NULL REFERENCES users (id),
    created_at  INTEGER NOT NULL,
    PRIMARY KEY (chat_id, chat_msg_id)
) STRICT;

CREATE INDEX support_msgs_created ON support_msgs (created_at);

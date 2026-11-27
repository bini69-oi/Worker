-- Смена тарифа со следующего периода и докупка трафика и устройств.

-- Ждущая смена: тариф, срок и уже уплаченная цена. Без внешнего ключа: тариф могут удалить, тогда смена отменяется с возвратом.
ALTER TABLE subscriptions ADD COLUMN next_tariff_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN next_days INTEGER NOT NULL DEFAULT 0 CHECK (next_days >= 0);
ALTER TABLE subscriptions ADD COLUMN next_price INTEGER NOT NULL DEFAULT 0 CHECK (next_price >= 0);
-- Докупленное: трафик до конца периода, устройства переходят в следующий период.
ALTER TABLE subscriptions ADD COLUMN extra_traffic_bytes INTEGER NOT NULL DEFAULT 0 CHECK (extra_traffic_bytes >= 0);
ALTER TABLE subscriptions ADD COLUMN extra_devices INTEGER NOT NULL DEFAULT 0 CHECK (extra_devices >= 0);

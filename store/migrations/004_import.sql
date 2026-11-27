-- Переезд из панели: подписка с лимитами и именем пользователя из панели, а не из нашего тарифа.
ALTER TABLE subscriptions ADD COLUMN imported INTEGER NOT NULL DEFAULT 0;
ALTER TABLE subscriptions ADD COLUMN own_username TEXT NOT NULL DEFAULT '';
ALTER TABLE subscriptions ADD COLUMN own_traffic_bytes INTEGER NOT NULL DEFAULT 0 CHECK (own_traffic_bytes >= 0);
ALTER TABLE subscriptions ADD COLUMN own_devices INTEGER NOT NULL DEFAULT 0 CHECK (own_devices >= 0);
ALTER TABLE subscriptions ADD COLUMN own_reset TEXT NOT NULL DEFAULT '';
ALTER TABLE subscriptions ADD COLUMN own_squads TEXT NOT NULL DEFAULT '[]'; -- JSON: сквады из панели
CREATE INDEX subscriptions_panel_uuid ON subscriptions (panel_uuid) WHERE panel_uuid <> '';

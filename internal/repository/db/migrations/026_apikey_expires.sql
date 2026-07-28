-- Extend api_keys with lifecycle fields for expiration and rotation reminders.

ALTER TABLE api_keys ADD COLUMN expires_at TIMESTAMP NULL;
ALTER TABLE api_keys ADD COLUMN rotated_at TIMESTAMP NULL;
ALTER TABLE api_keys ADD COLUMN rotation_reminder_sent INTEGER NOT NULL DEFAULT 0;

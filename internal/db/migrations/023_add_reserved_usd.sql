-- Add reserved_usd columns for budget pre-authorization mode
-- This eliminates the Check-Then-Act race by reserving budget before the request.

ALTER TABLE api_keys ADD COLUMN reserved_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN reserved_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE tenants ADD COLUMN reserved_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE virtual_keys ADD COLUMN reserved_usd REAL NOT NULL DEFAULT 0;

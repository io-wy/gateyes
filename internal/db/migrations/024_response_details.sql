-- Split large body fields from responses into a separate response_details table.
-- This keeps the hot-path list query lightweight while full bodies are fetched
-- only when a single response is retrieved.

CREATE TABLE IF NOT EXISTS response_details (
    response_id TEXT PRIMARY KEY REFERENCES responses(id) ON DELETE CASCADE,
    request_body  TEXT NOT NULL DEFAULT '',
    response_body TEXT NOT NULL DEFAULT '',
    route_trace_body TEXT NOT NULL DEFAULT ''
);

-- Migrate existing body data from responses into response_details.
INSERT INTO response_details (response_id, request_body, response_body, route_trace_body)
SELECT id, request_body, response_body, route_trace_body FROM responses
ON CONFLICT (response_id) DO NOTHING;

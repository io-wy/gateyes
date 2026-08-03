CREATE TABLE IF NOT EXISTS response_details (
	response_id TEXT PRIMARY KEY REFERENCES responses(id) ON DELETE CASCADE,
	request_body TEXT NOT NULL DEFAULT '',
	response_body TEXT NOT NULL DEFAULT '',
	route_trace_body TEXT NOT NULL DEFAULT ''
);

INSERT INTO response_details (response_id, request_body, response_body, route_trace_body)
SELECT id, request_body, response_body, route_trace_body FROM responses
ON CONFLICT (response_id) DO NOTHING;

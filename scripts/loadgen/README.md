# Gateyes Loadgen

Multi-scenario Python traffic generator for Gateyes gateway.

## Scenarios

| Scenario | QPS | Description |
|----------|-----|-------------|
| `chat` | 2.0 | Pure chat: 70% streaming + 30% non-stream, multi-model mix |
| `session` | 0.3 | Multi-turn conversation with same `session_id` (sticky routing) |
| `agent` | 0.1 | LangChain-style agent with tool-use loops |
| `embedding` | 0.2 | Batch embedding requests (`/v1/embeddings`) |
| `bad` | 0.1 | Error injection: bad auth, invalid JSON, unknown model, etc. |
| `spike` | burst | Every 5 min: 100 QPS for 20 seconds |

## Quick Start

```bash
cd scripts/loadgen
pip install -r requirements.txt
python -u main.py
```

Or use the wrapper:
```bash
./scripts/loadgen/run.sh
```

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `GATEWAY_URL` | `http://localhost:8028` | Gateway endpoint |

## Key Pool

All scenarios share the key pool defined in `config.py`.
By default they use `demo-key-001:demo-secret-001`.

**Important:** Ensure the key has sufficient quota (`quota` field in gateway config).
If you see repeated `429` / `quota_exceeded` responses, either:
- Increase the key quota in gateway config and reload
- Use multiple distinct keys in `config.py`

## Graceful Shutdown

Send `SIGINT` (Ctrl+C) or `SIGTERM` to stop all workers cleanly.

## Output

Structured JSON logs to stdout. Example:
```json
{"event": "session_complete", "session_id": "abc123", "turns": 4, "model": "LongCat-Flash-Chat", "level": "info"}
```

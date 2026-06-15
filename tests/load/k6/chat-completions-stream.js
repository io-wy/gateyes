import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import {
  DURATION_RAMP,
  DURATION_RAMP_DOWN,
  DURATION_STEADY,
  DURATION_STRESS,
  GATEYES_MODEL,
  GATEYES_URL,
  GATEYES_API_KEY,
  MAX_TOKENS,
  STRESS_CONCURRENCY,
  TARGET_CONCURRENCY,
  DEFAULT_HEADERS,
  makeChatPayload,
} from './constants.js';

const errorRate = new Rate('llm_errors');
const firstTokenLatency = new Trend('llm_first_token_latency_ms');
const streamDuration = new Trend('llm_stream_duration_ms');
const chunksPerStream = new Trend('llm_stream_chunks');
const streamIncomplete = new Counter('llm_stream_incomplete');

export const options = {
  stages: [
    { duration: DURATION_RAMP, target: TARGET_CONCURRENCY },
    { duration: DURATION_STEADY, target: TARGET_CONCURRENCY },
    { duration: DURATION_STRESS, target: STRESS_CONCURRENCY },
    { duration: DURATION_RAMP_DOWN, target: 0 },
  ],

  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<30000'],
    http_req_waiting: ['p(95)<3000'],
    llm_errors: ['rate<0.01'],
  },
};

export default function () {
  const payload = makeChatPayload({ model: GATEYES_MODEL, max_tokens: MAX_TOKENS, stream: true });

  const start = Date.now();
  const res = http.post(`${GATEYES_URL}/v1/chat/completions`, payload, {
    headers: DEFAULT_HEADERS,
    tags: { surface: 'chat_completions_stream', model: GATEYES_MODEL },
    responseType: 'text',
  });
  const total = Date.now() - start;

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
    'content-type is text/event-stream': (r) => {
      const ct = r.headers['Content-Type'] || r.headers['content-type'] || '';
      return ct.includes('text/event-stream') || ct.includes('application/json');
    },
    'stream ends with [DONE]': (r) => r.body && r.body.includes('data: [DONE]'),
  });

  errorRate.add(!ok);

  if (res.status === 200) {
    const chunks = parseSSEChunks(res.body);
    chunksPerStream.add(chunks.length);
    streamDuration.add(total);
    firstTokenLatency.add(res.timings.waiting);

    if (!res.body.includes('data: [DONE]')) {
      streamIncomplete.add(1);
    }
  }

  sleep(1);
}

function parseSSEChunks(body) {
  if (!body) return [];
  const lines = body.split('\n');
  const chunks = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith('data: ')) {
      const data = trimmed.slice(6).trim();
      if (data && data !== '[DONE]') {
        try {
          chunks.push(JSON.parse(data));
        } catch (e) {
          // Ignore malformed lines.
        }
      }
    }
  }
  return chunks;
}

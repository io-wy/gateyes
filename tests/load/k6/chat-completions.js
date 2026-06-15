import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
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
const requestLatency = new Trend('llm_request_latency_ms');
const outputTokens = new Trend('llm_output_tokens');

export const options = {
  // Ramp → steady → stress → ramp down. Adjust via env vars.
  stages: [
    { duration: DURATION_RAMP, target: TARGET_CONCURRENCY },
    { duration: DURATION_STEADY, target: TARGET_CONCURRENCY },
    { duration: DURATION_STRESS, target: STRESS_CONCURRENCY },
    { duration: DURATION_RAMP_DOWN, target: 0 },
  ],

  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<5000'],
    http_req_waiting: ['p(95)<3000'],
    llm_errors: ['rate<0.01'],
  },
};

export default function () {
  const payload = makeChatPayload({ model: GATEYES_MODEL, max_tokens: MAX_TOKENS });

  const res = http.post(`${GATEYES_URL}/v1/chat/completions`, payload, {
    headers: DEFAULT_HEADERS,
    tags: { surface: 'chat_completions', model: GATEYES_MODEL },
  });

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
    'response has choices': (r) => r.json('choices') !== null,
    'choices has content': (r) => {
      const choices = r.json('choices');
      return choices && choices[0] && choices[0].message && choices[0].message.content;
    },
    'usage is present': (r) => r.json('usage') !== null,
  });

  errorRate.add(!ok);

  if (res.status === 200) {
    requestLatency.add(res.timings.waiting);
    const usage = res.json('usage');
    if (usage && usage.completion_tokens) {
      outputTokens.add(usage.completion_tokens);
    }
  }

  sleep(1);
}

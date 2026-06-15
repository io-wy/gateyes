export const GATEYES_URL = __ENV.GATEYES_URL || 'http://localhost:8028';
export const GATEYES_API_KEY = __ENV.GATEYES_API_KEY || 'demo-key-001:demo-secret';
export const GATEYES_MODEL = __ENV.GATEYES_MODEL || 'mock-model';
export const MAX_TOKENS = parseInt(__ENV.GATEYES_MAX_TOKENS || '128', 10);
export const DURATION_RAMP = __ENV.GATEYES_DURATION_RAMP || '1m';
export const DURATION_STEADY = __ENV.GATEYES_DURATION_STEADY || '3m';
export const DURATION_STRESS = __ENV.GATEYES_DURATION_STRESS || '2m';
export const DURATION_RAMP_DOWN = __ENV.GATEYES_DURATION_RAMP_DOWN || '1m';
export const TARGET_CONCURRENCY = parseInt(__ENV.GATEYES_TARGET_CONCURRENCY || '100', 10);
export const STRESS_CONCURRENCY = parseInt(__ENV.GATEYES_STRESS_CONCURRENCY || '300', 10);

export const DEFAULT_HEADERS = {
  'Content-Type': 'application/json',
  Authorization: `Bearer ${GATEYES_API_KEY}`,
};

export function makeChatPayload(extra = {}) {
  return JSON.stringify({
    model: GATEYES_MODEL,
    messages: [
      { role: 'system', content: 'You are a helpful assistant.' },
      { role: 'user', content: 'Summarize load testing in one sentence.' },
    ],
    max_tokens: MAX_TOKENS,
    stream: false,
    ...extra,
  });
}

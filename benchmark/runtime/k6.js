import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const apiKey = __ENV.GATEYES_API_KEY || '';
const apiSecret = __ENV.GATEYES_API_SECRET || '';
const baseURL = __ENV.GATEYES_URL || 'http://localhost:8083';

const errorRate = new Rate('error_rate');
const chatLatency = new Trend('chat_latency');
const responseLatency = new Trend('response_latency');
const embeddingLatency = new Trend('embedding_latency');

export const options = {
  stages: [
    { duration: '1m', target: 10 },
    { duration: '2m', target: 50 },
    { duration: '2m', target: 100 },
    { duration: '1m', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<30000'],
    error_rate: ['rate<0.1'],
  },
};

function request(endpoint, body, latencyTrend) {
  const res = http.post(`${baseURL}${endpoint}`, JSON.stringify(body), {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${apiKey}`,
      'X-API-Secret': apiSecret,
    },
  });

  latencyTrend.add(res.timings.duration);
  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
  });

  errorRate.add(!ok);
  return res;
}

export default function () {
  const scenario = Math.random();

  if (scenario < 0.70) {
    request('/v1/chat/completions', {
      model: 'gpt-4o',
      messages: [{ role: 'user', content: 'Say hello in 5 words' }],
      max_tokens: 50,
    }, chatLatency);
  } else if (scenario < 0.90) {
    request('/v1/responses', {
      model: 'gpt-4o',
      input: 'What is 2+2?',
      max_output_tokens: 50,
    }, responseLatency);
  } else {
    request('/v1/embeddings', {
      model: 'text-embedding-3-small',
      input: 'hello world',
    }, embeddingLatency);
  }

  sleep(Math.random() * 0.5);
}

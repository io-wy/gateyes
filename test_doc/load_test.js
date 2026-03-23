import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// 自定义指标
const errorRate = new Rate('errors');
const latency = new Trend('latency');

export const options = {
  scenarios: {
    // 场景1: 基准测试 - 10 VUs, 30s
    baseline: {
      executor: 'constant-vus',
      vus: 10,
      duration: '30s',
      tags: { test_type: 'baseline' },
    },
    // 场景2: 峰值测试 - 50 VUs, 30s
    peak: {
      executor: 'ramping-vus',
      startVUs: 10,
      stages: [
        { duration: '10s', target: 50 },
        { duration: '20s', target: 50 },
        { duration: '10s', target: 0 },
      ],
      tags: { test_type: 'peak' },
    },
    // 场景3: 突发测试 - 100 VUs, 20s
    burst: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '5s', target: 100 },
        { duration: '10s', target: 100 },
        { duration: '5s', target: 0 },
      ],
      tags: { test_type: 'burst' },
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.05'],
    errors: ['rate<0.1'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8083';
const API_KEY = __ENV.API_KEY || 'test-key-001:test-secret';
const MODEL = __ENV.MODEL || 'MiniMax-M2.5';

const headers = {
  'Authorization': `Bearer ${API_KEY}`,
  'Content-Type': 'application/json',
};

export default function () {
  // 测试 OpenAI Chat Completions
  testOpenAIChat();

  // 测试 Anthropic Messages
  testAnthropicMessages();

  // 模拟用户思考时间
  sleep(0.5);
}

function testOpenAIChat() {
  const url = `${BASE_URL}/v1/chat/completions`;
  const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: 'user', content: 'say hi' }],
    max_tokens: 50,
  });

  const res = http.post(url, payload, { headers });

  latency.add(res.timings.duration);

  const success = check(res, {
    'openai chat status is 200': (r) => r.status === 200,
    'openai chat has choices': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.choices && body.choices.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  errorRate.add(success ? 0 : 1);
}

function testAnthropicMessages() {
  const url = `${BASE_URL}/v1/messages`;
  const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: 'user', content: 'say hi' }],
    max_tokens: 50,
  });

  const res = http.post(url, payload, { headers });

  latency.add(res.timings.duration);

  const success = check(res, {
    'anthropic messages status is 200': (r) => r.status === 200,
    'anthropic messages has content': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.content && body.content.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  errorRate.add(success ? 0 : 1);
}

// 简单的流式测试
export function handleSummary(data) {
  return {
    'stdout': textSummary(data),
    './test_doc/k6_report.json': JSON.stringify(data),
  };
}

function textSummary(data) {
  const metrics = data.metrics;
  let summary = '\n========== K6 Load Test Summary ==========\n\n';

  if (metrics.http_req_duration) {
    const d = metrics.http_req_duration;
    summary += `Latency:\n`;
    summary += `  avg: ${d.values.avg.toFixed(2)} ms\n`;
    summary += `  p95: ${d.values['p(95)'].toFixed(2)} ms\n`;
    summary += `  p99: ${d.values['p(99)'].toFixed(2)} ms\n`;
    summary += `  max: ${d.values.max.toFixed(2)} ms\n\n`;
  }

  if (metrics.http_reqs) {
    summary += `Requests: ${metrics.http_reqs.values.count}\n`;
    summary += `RPS: ${metrics.http_reqs.values.rate.toFixed(2)}\n\n`;
  }

  if (metrics.http_req_failed) {
    const f = metrics.http_req_failed.values;
    summary += `Failures: ${(f.rate * 100).toFixed(2)}%\n`;
  }

  summary += '\n===========================================\n';
  return summary;
}

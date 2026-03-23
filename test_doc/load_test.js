import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// 自定义指标
const errorRate = new Rate('errors');
const latency = new Trend('latency');
const apiErrors = new Counter('api_errors');
const rateLimited = new Counter('rate_limited');

export const options = {
  scenarios: {
    // 场景1: 基准测试 - 5 VUs, 30s
    baseline: {
      executor: 'constant-vus',
      vus: 5,
      duration: '30s',
    },
    // 场景2: 增长测试 - 逐步增加到 30 VUs
    rampup: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '10s', target: 15 },
        { duration: '10s', target: 15 },
        { duration: '10s', target: 30 },
        { duration: '20s', target: 30 },
        { duration: '10s', target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<1000', 'p(99)<2000'],
    http_req_failed: ['rate<0.1'],
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
  sleep(0.3);
}

function testOpenAIChat() {
  const url = `${BASE_URL}/v1/chat/completions`;
  const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: 'user', content: 'say hi' }],
    max_tokens: 20,
  });

  const res = http.post(url, payload, { headers });

  latency.add(res.timings.duration);

  // 检查响应状态
  if (res.status !== 200) {
    apiErrors.add(1);
    if (res.status === 429) {
      rateLimited.add(1);
    }
  }

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
    max_tokens: 20,
  });

  const res = http.post(url, payload, { headers });

  latency.add(res.timings.duration);

  // 检查响应状态
  if (res.status !== 200) {
    apiErrors.add(1);
    if (res.status === 429) {
      rateLimited.add(1);
    }
  }

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

// 生成测试报告
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
    const d = metrics.http_req_duration.values;
    summary += `Latency:\n`;
    summary += `  avg: ${d.avg.toFixed(2)} ms\n`;
    summary += `  p95: ${d['p(95)'].toFixed(2)} ms\n`;
    summary += `  p99: ${d['p(99)'].toFixed(2)} ms\n`;
    summary += `  max: ${d.max.toFixed(2)} ms\n\n`;
  }

  if (metrics.http_reqs) {
    summary += `Requests: ${metrics.http_reqs.values.count}\n`;
    summary += `RPS: ${metrics.http_reqs.values.rate.toFixed(2)}\n\n`;
  }

  if (metrics.http_req_failed) {
    const f = metrics.http_req_failed.values;
    summary += `Failures: ${(f.rate * 100).toFixed(2)}%\n`;
  }

  if (metrics.api_errors) {
    summary += `API Errors: ${metrics.api_errors.values.count}\n`;
  }

  if (metrics.rate_limited) {
    summary += `Rate Limited (429): ${metrics.rate_limited.values.count}\n`;
  }

  summary += '\n===========================================\n';
  return summary;
}

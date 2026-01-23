// k6 Stress Test Script for Embedding Service
// Purpose: Find the breaking point of the embedding service
// Usage: k6 run tests/load/embedding_stress_test.ts
//
// This test gradually increases load until the service fails
// to meet performance thresholds, identifying the capacity limit.
//
// Note: k6 1.0+ supports TypeScript natively without transpilation

import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Type definitions
interface GrpcResponse {
  status: number;
  message?: unknown;
}

interface SearchSimilarRequest {
  embedding: number[];
  embedding_type: string;
  limit: number;
}

interface MetricValues {
  'p(95)'?: number;
  rate?: number;
  count?: number;
}

interface Metric {
  values?: MetricValues;
}

interface SummaryData {
  metrics: {
    response_time?: Metric;
    errors?: Metric;
    slow_responses?: Metric;
    requests?: Metric;
    successes?: Metric;
  };
}

// Custom metrics
const responseTime = new Trend('response_time');
const errorRate = new Rate('errors');
const slowResponseRate = new Rate('slow_responses');
const requestCount = new Counter('requests');
const successCount = new Counter('successes');

// Threshold constants for consistency
const P95_THRESHOLD_MS = 2000;
const ERROR_RATE_THRESHOLD = 0.10;

// Stress test configuration - aggressive ramp up
export const options = {
  stages: [
    { duration: '1m', target: 50 },    // Warm up
    { duration: '2m', target: 100 },   // Normal load
    { duration: '2m', target: 200 },   // High load
    { duration: '2m', target: 300 },   // Very high load
    { duration: '2m', target: 400 },   // Stress load
    { duration: '2m', target: 500 },   // Breaking point discovery
    { duration: '3m', target: 500 },   // Sustained stress
    { duration: '2m', target: 0 },     // Recovery
  ],
  thresholds: {
    // Stress test thresholds - more lenient to find breaking point
    'response_time': [`p(95)<${P95_THRESHOLD_MS}`],
    'errors': [`rate<${ERROR_RATE_THRESHOLD}`],
  },
};

const client = new grpc.Client();
// Path is relative to script location: tests/load/ -> ../../proto/embedding/
client.load(['../../proto/embedding'], 'embedding.proto');

const GRPC_HOST: string = __ENV.GRPC_HOST || 'localhost:50052';

// Track connection state per VU
let isConnected = false;

function generateEmbedding(dimension: number): number[] {
  const embedding: number[] = [];
  for (let i = 0; i < dimension; i++) {
    embedding.push(Math.random() * 2 - 1);
  }
  return embedding;
}

// Setup runs once per VU at the start
export function setup(): void {
  console.log('Starting Embedding Service Stress Test');
  console.log(`Target: ${GRPC_HOST}`);
  console.log(`P95 Threshold: ${P95_THRESHOLD_MS}ms`);
  console.log(`Error Rate Threshold: ${ERROR_RATE_THRESHOLD * 100}%`);
}

export default function (): void {
  // Connect once per VU, reconnect if needed
  if (!isConnected) {
    try {
      client.connect(GRPC_HOST, { plaintext: true });
      isConnected = true;
    } catch {
      errorRate.add(1);
      return;
    }
  }

  requestCount.add(1);

  const embedding: number[] = generateEmbedding(1536);
  const start: number = Date.now();

  // Focus on the most intensive operation: SearchSimilar
  const request: SearchSimilarRequest = {
    embedding: embedding,
    embedding_type: 'content',
    limit: 100,  // Maximum limit for stress
  };

  let response: GrpcResponse;
  try {
    response = client.invoke(
      'embedding.EmbeddingService/SearchSimilar',
      request
    ) as GrpcResponse;
  } catch {
    // Connection error - mark as error and try to reconnect next iteration
    errorRate.add(1);
    slowResponseRate.add(0);
    isConnected = false;
    return;
  }

  const duration: number = Date.now() - start;
  responseTime.add(duration);

  // Check status separately from timing
  const statusOk: boolean = check(response, {
    'status is OK': (r: GrpcResponse) => r && r.status === grpc.StatusOK,
  });

  // Track slow responses separately
  const isSlow = duration >= P95_THRESHOLD_MS;
  slowResponseRate.add(isSlow ? 1 : 0);

  // Error rate only counts actual errors, not slow responses
  if (statusOk) {
    successCount.add(1);
    errorRate.add(0);
  } else {
    errorRate.add(1);
  }

  sleep(0.05);  // Minimal sleep to maximize load
}

// Teardown runs once per VU at the end
export function teardown(): void {
  if (isConnected) {
    client.close();
    isConnected = false;
  }
}

export function handleSummary(data: SummaryData): { stdout: string } {
  // Custom summary to identify breaking point
  // Safely extract metrics with null checks
  const responseTimeMetric = data.metrics.response_time;
  const p95: number | null = responseTimeMetric?.values?.['p(95)'] ?? null;
  const errorRateValue: number = data.metrics.errors?.values?.rate ?? 0;
  const slowResponseRateValue: number = data.metrics.slow_responses?.values?.rate ?? 0;
  const totalRequests: number = data.metrics.requests?.values?.count ?? 0;
  const successfulRequests: number = data.metrics.successes?.values?.count ?? 0;

  console.log('\n=== STRESS TEST RESULTS ===');
  console.log(`Total Requests: ${totalRequests}`);
  console.log(`Successful Requests: ${successfulRequests}`);
  console.log(`Error Rate: ${(errorRateValue * 100).toFixed(2)}%`);
  console.log(`Slow Response Rate: ${(slowResponseRateValue * 100).toFixed(2)}%`);
  console.log(`P95 Response Time: ${p95 !== null ? p95.toFixed(2) : 'N/A'}ms`);

  // Determine if breaking point was reached using same thresholds as options
  const p95Exceeded: boolean = p95 !== null && p95 > P95_THRESHOLD_MS;
  const errorRateExceeded: boolean = errorRateValue > ERROR_RATE_THRESHOLD;

  if (errorRateExceeded || p95Exceeded) {
    console.log('\n⚠️ BREAKING POINT IDENTIFIED');
    if (errorRateExceeded) {
      console.log(`  - Error rate (${(errorRateValue * 100).toFixed(2)}%) exceeded threshold (${ERROR_RATE_THRESHOLD * 100}%)`);
    }
    if (p95Exceeded) {
      console.log(`  - P95 response time (${p95?.toFixed(2)}ms) exceeded threshold (${P95_THRESHOLD_MS}ms)`);
    }
    console.log('Service degradation detected under stress');
  } else {
    console.log('\n✅ SERVICE RESILIENT');
    console.log('No breaking point reached at maximum load');
  }

  return {
    'stdout': JSON.stringify(data, null, 2),
  };
}

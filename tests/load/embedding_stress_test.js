// k6 Stress Test Script for Embedding Service
// Purpose: Find the breaking point of the embedding service
// Usage: k6 run tests/load/embedding_stress_test.js
//
// This test gradually increases load until the service fails
// to meet performance thresholds, identifying the capacity limit.

import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const responseTime = new Trend('response_time');
const errorRate = new Rate('errors');
const requestCount = new Counter('requests');
const successCount = new Counter('successes');

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
    'response_time': ['p(95)<2000'],  // 95% under 2 seconds
    'errors': ['rate<0.10'],          // Allow up to 10% errors
  },
};

const client = new grpc.Client();
client.load(['proto/embedding'], 'embedding.proto');

const GRPC_HOST = __ENV.GRPC_HOST || 'localhost:50051';

function generateEmbedding(dimension) {
  const embedding = [];
  for (let i = 0; i < dimension; i++) {
    embedding.push(Math.random() * 2 - 1);
  }
  return embedding;
}

export default function () {
  client.connect(GRPC_HOST, { plaintext: true });
  requestCount.add(1);

  const embedding = generateEmbedding(1536);
  const start = Date.now();

  // Focus on the most intensive operation: SearchSimilar
  const response = client.invoke('embedding.EmbeddingService/SearchSimilar', {
    embedding: embedding,
    embedding_type: 'content',
    limit: 100,  // Maximum limit for stress
  });

  const duration = Date.now() - start;
  responseTime.add(duration);

  const success = check(response, {
    'status is OK': (r) => r && r.status === grpc.StatusOK,
    'response within 2s': () => duration < 2000,
  });

  if (success) {
    successCount.add(1);
    errorRate.add(0);  // Add 0 to properly compute error rate
  } else {
    errorRate.add(1);
  }

  client.close();
  sleep(0.05);  // Minimal sleep to maximize load
}

export function handleSummary(data) {
  // Custom summary to identify breaking point
  // Safely extract metrics with null checks
  const responseTimeMetric = data.metrics.response_time;
  const p95 = responseTimeMetric && responseTimeMetric.values ? responseTimeMetric.values['p(95)'] : null;
  const errorRateValue = data.metrics.errors ? data.metrics.errors.values.rate : 0;
  const totalRequests = data.metrics.requests ? data.metrics.requests.values.count : 0;
  const successfulRequests = data.metrics.successes ? data.metrics.successes.values.count : 0;

  console.log('\n=== STRESS TEST RESULTS ===');
  console.log(`Total Requests: ${totalRequests}`);
  console.log(`Successful Requests: ${successfulRequests}`);
  console.log(`Error Rate: ${(errorRateValue * 100).toFixed(2)}%`);
  console.log(`P95 Response Time: ${p95 !== null ? p95.toFixed(2) : 'N/A'}ms`);

  // Determine if breaking point was reached
  const p95Exceeded = p95 !== null && p95 > 1000;
  if (errorRateValue > 0.05 || p95Exceeded) {
    console.log('\n⚠️ BREAKING POINT IDENTIFIED');
    console.log('Service degradation detected under stress');
  } else {
    console.log('\n✅ SERVICE RESILIENT');
    console.log('No breaking point reached at maximum load');
  }

  return {
    'stdout': JSON.stringify(data, null, 2),
  };
}

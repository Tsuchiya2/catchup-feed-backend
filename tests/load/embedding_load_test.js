// k6 Load Test Script for Embedding Service
// Usage: k6 run --vus 10 --duration 30s tests/load/embedding_load_test.js
//
// Prerequisites:
// 1. Install k6: brew install k6
// 2. Install k6 gRPC extension: xk6 build --with github.com/grafana/xk6-grpc
// 3. Start the embedding service
// 4. Run: k6 run tests/load/embedding_load_test.js

import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Custom metrics
const storeEmbeddingDuration = new Trend('store_embedding_duration');
const getEmbeddingsDuration = new Trend('get_embeddings_duration');
const searchSimilarDuration = new Trend('search_similar_duration');
const errorRate = new Rate('errors');

// Test configuration
export const options = {
  stages: [
    // Ramp up
    { duration: '30s', target: 10 },  // Ramp up to 10 users
    { duration: '1m', target: 10 },   // Stay at 10 users
    { duration: '30s', target: 50 },  // Ramp up to 50 users
    { duration: '2m', target: 50 },   // Stay at 50 users
    { duration: '30s', target: 100 }, // Ramp up to 100 users
    { duration: '2m', target: 100 },  // Stay at 100 users
    // Ramp down
    { duration: '1m', target: 0 },    // Ramp down
  ],
  thresholds: {
    // Performance targets
    'store_embedding_duration': ['p(95)<100', 'p(99)<500'],  // 95% < 100ms, 99% < 500ms
    'get_embeddings_duration': ['p(95)<50', 'p(99)<200'],    // 95% < 50ms, 99% < 200ms
    'search_similar_duration': ['p(95)<200', 'p(99)<1000'],  // 95% < 200ms, 99% < 1s
    'errors': ['rate<0.01'],  // Error rate < 1%
  },
};

const client = new grpc.Client();
client.load(['proto/embedding'], 'embedding.proto');

const GRPC_HOST = __ENV.GRPC_HOST || 'localhost:50051';

// Generate random embedding vector
function generateEmbedding(dimension) {
  const embedding = [];
  for (let i = 0; i < dimension; i++) {
    embedding.push(Math.random() * 2 - 1);  // Values between -1 and 1
  }
  return embedding;
}

export default function () {
  client.connect(GRPC_HOST, { plaintext: true });

  const articleId = Math.floor(Math.random() * 10000) + 1;
  const embedding = generateEmbedding(1536);

  // Test 1: StoreEmbedding
  const storeStart = Date.now();
  const storeResponse = client.invoke('embedding.EmbeddingService/StoreEmbedding', {
    article_id: articleId,
    embedding_type: 'content',
    provider: 'openai',
    model: 'text-embedding-3-small',
    dimension: 1536,
    embedding: embedding,
  });
  storeEmbeddingDuration.add(Date.now() - storeStart);

  check(storeResponse, {
    'StoreEmbedding status is OK': (r) => r && r.status === grpc.StatusOK,
    'StoreEmbedding success': (r) => r && r.message && r.message.success === true,
  }) || errorRate.add(1);

  sleep(0.1);

  // Test 2: GetEmbeddings
  const getStart = Date.now();
  const getResponse = client.invoke('embedding.EmbeddingService/GetEmbeddings', {
    article_id: articleId,
  });
  getEmbeddingsDuration.add(Date.now() - getStart);

  check(getResponse, {
    'GetEmbeddings status is OK': (r) => r && r.status === grpc.StatusOK,
  }) || errorRate.add(1);

  sleep(0.1);

  // Test 3: SearchSimilar
  const searchStart = Date.now();
  const searchResponse = client.invoke('embedding.EmbeddingService/SearchSimilar', {
    embedding: embedding,
    embedding_type: 'content',
    limit: 10,
  });
  searchSimilarDuration.add(Date.now() - searchStart);

  check(searchResponse, {
    'SearchSimilar status is OK': (r) => r && r.status === grpc.StatusOK,
  }) || errorRate.add(1);

  sleep(0.1);

  client.close();
}

// Lifecycle hooks
export function setup() {
  console.log('Starting Embedding Service Load Test');
  console.log(`Target: ${GRPC_HOST}`);
}

export function teardown(data) {
  console.log('Load Test Complete');
}

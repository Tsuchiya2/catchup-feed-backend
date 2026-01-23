// k6 Load Test Script for Embedding Service
// Usage: k6 run --vus 10 --duration 30s tests/load/embedding_load_test.ts
//
// Prerequisites:
// 1. Install k6: brew install k6
// 2. Start the embedding service
// 3. Run: k6 run tests/load/embedding_load_test.ts
//
// Note: k6 1.0+ supports TypeScript natively without transpilation

import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// Type definitions for k6 gRPC
interface GrpcResponse {
  status: number;
  message?: {
    success?: boolean;
    embeddings?: unknown[];
    results?: unknown[];
  };
}

interface StoreEmbeddingRequest {
  article_id: number;
  embedding_type: string;
  provider: string;
  model: string;
  dimension: number;
  embedding: number[];
}

interface GetEmbeddingsRequest {
  article_id: number;
}

interface SearchSimilarRequest {
  embedding: number[];
  embedding_type: string;
  limit: number;
}

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
// Path is relative to script location: tests/load/ -> ../../proto/embedding/
client.load(['../../proto/embedding'], 'embedding.proto');

const GRPC_HOST: string = __ENV.GRPC_HOST || 'localhost:50052';

// Track connection state per VU
let isConnected = false;

// Generate random embedding vector
function generateEmbedding(dimension: number): number[] {
  const embedding: number[] = [];
  for (let i = 0; i < dimension; i++) {
    embedding.push(Math.random() * 2 - 1);  // Values between -1 and 1
  }
  return embedding;
}

// Lifecycle hooks
export function setup(): void {
  console.log('Starting Embedding Service Load Test');
  console.log(`Target: ${GRPC_HOST}`);
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

  const articleId: number = Math.floor(Math.random() * 10000) + 1;
  const embedding: number[] = generateEmbedding(1536);

  // Test 1: StoreEmbedding
  const storeStart: number = Date.now();
  const storeRequest: StoreEmbeddingRequest = {
    article_id: articleId,
    embedding_type: 'content',
    provider: 'openai',
    model: 'text-embedding-3-small',
    dimension: 1536,
    embedding: embedding,
  };

  let storeResponse: GrpcResponse;
  try {
    storeResponse = client.invoke(
      'embedding.EmbeddingService/StoreEmbedding',
      storeRequest
    ) as GrpcResponse;
    storeEmbeddingDuration.add(Date.now() - storeStart);

    check(storeResponse, {
      'StoreEmbedding status is OK': (r: GrpcResponse) => r && r.status === grpc.StatusOK,
      'StoreEmbedding success': (r: GrpcResponse) => r && r.message?.success === true,
    }) || errorRate.add(1);
  } catch {
    errorRate.add(1);
    isConnected = false;
    return;
  }

  sleep(0.1);

  // Test 2: GetEmbeddings
  const getStart: number = Date.now();
  const getRequest: GetEmbeddingsRequest = {
    article_id: articleId,
  };

  let getResponse: GrpcResponse;
  try {
    getResponse = client.invoke(
      'embedding.EmbeddingService/GetEmbeddings',
      getRequest
    ) as GrpcResponse;
    getEmbeddingsDuration.add(Date.now() - getStart);

    check(getResponse, {
      'GetEmbeddings status is OK': (r: GrpcResponse) => r && r.status === grpc.StatusOK,
    }) || errorRate.add(1);
  } catch {
    errorRate.add(1);
    isConnected = false;
    return;
  }

  sleep(0.1);

  // Test 3: SearchSimilar
  const searchStart: number = Date.now();
  const searchRequest: SearchSimilarRequest = {
    embedding: embedding,
    embedding_type: 'content',
    limit: 10,
  };

  let searchResponse: GrpcResponse;
  try {
    searchResponse = client.invoke(
      'embedding.EmbeddingService/SearchSimilar',
      searchRequest
    ) as GrpcResponse;
    searchSimilarDuration.add(Date.now() - searchStart);

    check(searchResponse, {
      'SearchSimilar status is OK': (r: GrpcResponse) => r && r.status === grpc.StatusOK,
    }) || errorRate.add(1);
  } catch {
    errorRate.add(1);
    isConnected = false;
    return;
  }

  sleep(0.1);
}

export function teardown(): void {
  if (isConnected) {
    client.close();
    isConnected = false;
  }
  console.log('Load Test Complete');
}

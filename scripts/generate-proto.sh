#!/bin/sh
# ============================================================
# Proto Code Generation Script
# ============================================================
# Generates Go code from Protocol Buffer definitions.
# Run this script when proto files are modified.
#
# Usage (from project root):
#   make proto
# ============================================================

set -e

echo "==> Generating Go code from proto files..."

# Clean up old generated files
rm -rf internal/interface/grpc/pb/embedding

# Ensure output directory exists
mkdir -p internal/interface/grpc/pb/embedding

# Generate Go code from embedding.proto
protoc \
  --proto_path=proto/embedding \
  --go_out=internal/interface/grpc/pb/embedding \
  --go_opt=paths=source_relative \
  --go-grpc_out=internal/interface/grpc/pb/embedding \
  --go-grpc_opt=paths=source_relative \
  embedding.proto

echo "==> Proto generation complete!"
echo "    Generated files:"
ls -la internal/interface/grpc/pb/embedding/

# Environment Configuration Templates

This directory contains environment-specific configuration templates for different deployment scenarios.

## Available Templates

| File | Environment | Use Case | Rate Limit Settings |
|------|-------------|----------|---------------------|
| `development.env` | Local Development | Developer laptops, Docker Compose | Relaxed (1000 IP, 10000 user/hr) |
| `staging.env` | Staging/Testing | Pre-production testing | Moderate (500 IP, 5000 user/hr) |
| `production.env` | Production | Live deployment | Strict (100 IP, 1000 user/hr) |

## Quick Start

### Development

```bash
# Copy template to project root
cp config/environments/development.env .env

# Update required variables (API keys, passwords)
# See .env.example for variable descriptions

# Start application
docker compose up -d
```

### Staging

```bash
cp config/environments/staging.env .env.staging
# Configure staging-specific values
# Deploy to staging infrastructure
```

### Production

**DO NOT copy production.env directly!** Use it as a reference only.

Set environment variables through your deployment platform:
- Docker: Use `.env` file or environment variables in `docker-compose.yml`
- Kubernetes: Use ConfigMaps and Secrets
- Cloud: Use platform secret management (AWS Secrets Manager, etc.)

## Configuration Comparison

### Rate Limiting Settings

| Setting | Development | Staging | Production | Notes |
|---------|-------------|---------|------------|-------|
| **IP Limit** | 1000/min | 500/min | 100/min | Requests per IP address |
| **User Limit** | 10000/hr | 5000/hr | 1000/hr | Requests per authenticated user |
| **Admin Tier** | 50000/hr | 25000/hr | 10000/hr | Admin users |
| **Premium Tier** | 25000/hr | 12500/hr | 5000/hr | Premium users |
| **Basic Tier** | 10000/hr | 5000/hr | 1000/hr | Standard users |
| **Viewer Tier** | 5000/hr | 2500/hr | 500/hr | Read-only users |
| **Max Keys** | 5000 | 10000 | 10000 | Memory limit |
| **Cleanup** | 2min | 5min | 5min | Cleanup interval |

### CSP Settings

| Setting | Development | Staging | Production | Notes |
|---------|-------------|---------|------------|-------|
| **Enabled** | ✅ Yes | ✅ Yes | ✅ Yes | CSP headers enabled |
| **Report Only** | ✅ Yes | ❌ No | ❌ No | Report violations without blocking |

**Development**: Report-only mode allows testing without breaking functionality.

**Staging/Production**: Enforce CSP to prevent XSS attacks.

### Circuit Breaker

Same across all environments:
- **Failure Threshold**: 10 consecutive failures
- **Recovery Timeout**: 30 seconds

## File Descriptions

### development.env

**Purpose**: Local development with Docker Compose

**Key Features**:
- Higher rate limits (1000 IP/min, 10000 user/hr) for easier testing
- More frequent cleanup (2 minutes) for testing memory management
- CSP in report-only mode to avoid blocking during development
- Includes localhost/Docker network ranges in trusted proxies
- Detailed comments with testing recommendations

**Recommended For**:
- Developer laptops
- Feature development
- Unit/integration testing
- Debugging rate limiting logic

### staging.env

**Purpose**: Pre-production testing environment

**Key Features**:
- Moderate rate limits (500 IP/min, 5000 user/hr) for realistic testing
- CSP enforced (not report-only) to test production behavior
- Production-like settings but more permissive for load testing
- Includes testing checklist and validation guide

**Recommended For**:
- QA testing
- Load testing
- Integration testing with external services
- Final validation before production deployment

### production.env

**Purpose**: Production deployment reference (NOT a template to copy)

**Key Features**:
- Strict rate limits (100 IP/min, 1000 user/hr) for production security
- CSP fully enforced
- Comprehensive security checklist
- Monitoring and alerting configuration guidance
- Production-specific recommendations

**Important**: This is a **reference file** only. Do NOT copy it directly. Use your deployment platform's secret management instead.

## Usage Guidelines

### 1. Development Workflow

```bash
# Initial setup
cp config/environments/development.env .env

# Edit required variables
vim .env  # or nano, code, etc.

# Required changes:
# - ANTHROPIC_API_KEY (or OPENAI_API_KEY)
# - ADMIN_USER_PASSWORD
# - JWT_SECRET

# Start application
docker compose up -d

# Test rate limiting
for i in {1..150}; do curl http://localhost:8080/articles; done
# Request #1001+ should return 429 (development has 1000/min limit)
```

### 2. Staging Deployment

```bash
# Copy staging template
cp config/environments/staging.env .env.staging

# Update staging-specific values
# - Database credentials (staging DB)
# - API keys (staging/test accounts)
# - Trusted proxies (staging infrastructure)

# Deploy to staging
# (deployment method depends on your infrastructure)
```

### 3. Production Deployment

**Method 1: Docker Compose (Simple)**

```bash
# Create production .env (don't copy production.env directly!)
# Use strong, unique values for all secrets

# Required variables:
cat > .env << 'EOF'
POSTGRES_USER=catchup_prod
POSTGRES_PASSWORD=$(openssl rand -base64 32)
POSTGRES_DB=catchup_prod
DATABASE_URL=postgres://catchup_prod:...@postgres:5432/catchup_prod?sslmode=disable

ANTHROPIC_API_KEY=sk-ant-prod-xxxxxxxx
JWT_SECRET=$(openssl rand -base64 64)
ADMIN_USER=admin
ADMIN_USER_PASSWORD=$(openssl rand -base64 24)

RATELIMIT_ENABLED=true
RATELIMIT_IP_LIMIT=100
RATELIMIT_USER_LIMIT=1000
# ... see production.env for complete list

CSP_ENABLED=true
CSP_REPORT_ONLY=false
EOF

# Deploy
docker compose -f docker-compose.prod.yml up -d
```

**Method 2: Kubernetes (Recommended for Production)**

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  RATELIMIT_ENABLED: "true"
  RATELIMIT_IP_LIMIT: "100"
  CSP_ENABLED: "true"
  # ... non-sensitive config

---
# secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: app-secrets
type: Opaque
stringData:
  POSTGRES_PASSWORD: <encrypted>
  JWT_SECRET: <encrypted>
  ANTHROPIC_API_KEY: <encrypted>
```

**Method 3: Cloud Platform (AWS, GCP, Azure)**

Use platform-specific secret management:
- **AWS**: AWS Secrets Manager + Parameter Store
- **GCP**: Secret Manager
- **Azure**: Key Vault

## Validation Checklist

Before deploying to production, verify:

### Required Variables ✅

- [ ] `POSTGRES_USER` set to non-default value
- [ ] `POSTGRES_PASSWORD` is strong (min 12 chars)
- [ ] `DATABASE_URL` matches database credentials
- [ ] `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` configured
- [ ] `JWT_SECRET` is random and ≥32 chars
- [ ] `ADMIN_USER` set to non-default value
- [ ] `ADMIN_USER_PASSWORD` meets security requirements (≥12 chars, not weak)

### Rate Limiting ✅

- [ ] `RATELIMIT_ENABLED=true`
- [ ] `RATELIMIT_IP_LIMIT` set appropriately for your traffic
- [ ] `RATELIMIT_USER_LIMIT` set appropriately for your traffic
- [ ] `RATELIMIT_TRUSTED_PROXIES` matches your infrastructure
- [ ] Tier limits configured for your user tiers

### Security ✅

- [ ] `CSP_ENABLED=true`
- [ ] `CSP_REPORT_ONLY=false` (production only)
- [ ] No weak passwords or default credentials
- [ ] All secrets are unique (not reused from development/staging)

### Monitoring ✅

- [ ] Prometheus metrics endpoint accessible
- [ ] Alert rules deployed
- [ ] Health checks configured
- [ ] Log aggregation collecting rate limit events

## Troubleshooting

### Environment Variables Not Loaded

**Symptom**: Application uses default values instead of configured values

**Solution**:
```bash
# Verify .env file location
ls -la .env

# Check Docker Compose picks up .env
docker compose config | grep RATELIMIT

# Restart application
docker compose restart app
```

### Rate Limiting Too Strict

**Symptom**: Legitimate traffic getting rate limited

**Quick Fix**:
```bash
# Temporarily increase limits
echo "RATELIMIT_IP_LIMIT=500" >> .env
docker compose restart app
```

**Long-term Solution**: Analyze traffic patterns and adjust limits accordingly.

### Cannot Find Configuration File

**Symptom**: Error: "config/environments/development.env not found"

**Solution**:
```bash
# Ensure you're in project root
pwd  # Should show: /path/to/catchup-feed

# Verify files exist
ls config/environments/

# If missing, restore from git
git checkout config/environments/
```

## Best Practices

### 1. Never Commit Secrets

```bash
# .gitignore already includes:
.env
.env.local
.env.*.local

# These are safe to commit (templates only):
config/environments/development.env
config/environments/staging.env
config/environments/production.env  # Reference only, no real secrets
```

### 2. Use Strong Random Values

```bash
# Generate secure JWT secret (64 bytes)
openssl rand -base64 64

# Generate secure password (24 bytes)
openssl rand -base64 24

# Generate UUID for database user
uuidgen | tr '[:upper:]' '[:lower:]'
```

### 3. Environment Isolation

Keep secrets completely separate:

| Environment | Password Example | JWT Secret Example |
|-------------|------------------|--------------------|
| Development | `dev-pass-123` | `dev-jwt-xxxxxx` |
| Staging | `stg-A8kL9mN2` | `stg-jwt-yyyyyy` |
| Production | `prod-X7$kM4#nP9` | `prod-jwt-zzzzzz` |

**Never** reuse production credentials in development/staging!

### 4. Rotate Secrets Regularly

```bash
# Production secret rotation schedule:
# - JWT_SECRET: Every 90 days
# - POSTGRES_PASSWORD: Every 90 days
# - API_KEYS: When vendor recommends or if compromised
# - ADMIN_USER_PASSWORD: Every 60 days
```

## See Also

- [Environment Setup Guide](../../docs/operations/environment-setup.md) - Detailed setup instructions
- [Rate Limiting Configuration](../../docs/operations/rate-limiting-configuration.md) - Rate limiting details
- [Security Best Practices](../../docs/security/best-practices.md) - Security guidelines
- `.env.example` - Complete variable reference

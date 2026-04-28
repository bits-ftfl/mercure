# Transport Configuration Guide

This guide explains how to configure Mercure to use either **Bolt** or **Redis** as the transport backend.

## Overview

| Feature | Bolt | Redis Standalone | Redis Cluster |
|---------|------|------------------|---------------|
| **Single Server** | ✅ | ✅ | ❌ |
| **Multiple Servers** | ❌ | ✅ | ✅ |
| **External Database** | ❌ | ✅ | ✅ |
| **Setup Complexity** | Minimal | Low | Medium |
| **High Availability** | ❌ | ✅ (with Sentinel) | ✅ |
| **Automatic Failover** | ❌ | ✅ (with Sentinel) | ✅ |
| **Memory Efficiency** | Good | Very Good | Very Good |
| **Network Overhead** | None | Network I/O | Network I/O |

## Bolt Transport (Default)

Bolt is an embedded key-value database perfect for single-server deployments.

### JSON Configuration

```json
{
  "transport": {
    "name": "bolt",
    "path": "/var/lib/mercure/hub.db",
    "bucket_name": "updates",
    "size": 10000,
    "cleanup_frequency": 0.3
  }
}
```

### Caddyfile Configuration

```caddyfile
mercure {
  anonymous
  demo
  ui

  transport bolt {
    path /var/lib/mercure/hub.db
    bucket_name updates
    size 10000
    cleanup_frequency 0.3
  }

  publisher_jwt {key}
  subscriber_jwt {key}
}
```

### Parameters

- **path**: Database file location (default: `{caddy_data_dir}/mercure.db`)
- **bucket_name**: Bolt bucket name for events (default: `updates`)
- **size**: Maximum number of events to keep (0 = unlimited)
- **cleanup_frequency**: Probability of cleanup (0-1, default: 0.3)

### Best For

- Single server deployments
- Development environments
- Low-traffic applications
- Scenarios where external dependencies should be minimized

---

## Redis Transport (Standalone)

Redis is an in-memory data store perfect for multi-server deployments.

### JSON Configuration

```json
{
  "transport": {
    "name": "redis",
    "addresses": ["redis.example.com:6379"],
    "password": "your-password",
    "db": 0,
    "key_prefix": "mercure:",
    "channel_prefix": "mercure:updates",
    "size": 50000,
    "cleanup_frequency": 0.3
  }
}
```

### Caddyfile Configuration

```caddyfile
mercure {
  anonymous
  demo
  ui

  transport redis {
    addresses redis.example.com:6379
    password your-password
    db 0
    key_prefix mercure:
    channel_prefix mercure:updates
    size 50000
    cleanup_frequency 0.3
  }

  publisher_jwt {key}
  subscriber_jwt {key}
}
```

### Parameters

- **addresses**: Redis server address(es) (default: `localhost:6379`)
- **password**: Redis authentication password (optional)
- **db**: Redis database number (only for standalone, default: 0)
- **key_prefix**: Prefix for all Redis keys (default: `mercure:`)
- **channel_prefix**: Pub/Sub channel name (default: `mercure:updates`)
- **size**: Maximum number of events (0 = unlimited)
- **cleanup_frequency**: Cleanup probability (0-1, default: 0.3)

### Best For

- Multi-server deployments
- High-traffic applications
- Scenarios requiring distributed updates
- Cloud deployments with managed Redis services

---

## Redis Transport (Cluster Mode)

Redis Cluster provides automatic sharding and high availability.

### JSON Configuration

```json
{
  "transport": {
    "name": "redis",
    "addresses": [
      "redis-node-1:6379",
      "redis-node-2:6379",
      "redis-node-3:6379"
    ],
    "password": "cluster-password",
    "key_prefix": "mercure:",
    "channel_prefix": "mercure:updates",
    "size": 100000,
    "cleanup_frequency": 0.5
  }
}
```

### Caddyfile Configuration

```caddyfile
mercure {
  anonymous
  demo
  ui

  transport redis {
    addresses redis-node-1:6379 redis-node-2:6379 redis-node-3:6379
    password cluster-password
    key_prefix mercure:
    channel_prefix mercure:updates
    size 100000
    cleanup_frequency 0.5
  }

  publisher_jwt {key}
  subscriber_jwt {key}
}
```

### Setup Requirements

1. **Install Redis Cluster** (minimum 3 master nodes)
   ```bash
   # Using Docker Compose (see examples below)
   docker-compose -f docker-compose.cluster.yml up -d
   ```

2. **Initialize Cluster**
   ```bash
   redis-cli --cluster create \
     127.0.0.1:6379 127.0.0.1:6380 127.0.0.1:6381 \
     --cluster-replicas 1
   ```

3. **Configure Mercure** with all node addresses

### Best For

- Large-scale deployments
- Enterprise environments
- Kubernetes deployments
- Maximum uptime requirements
- Automatic failover scenarios

---

## Docker Compose Examples

### Bolt (Single Container)

```yaml
# docker-compose.bolt.yml
version: '3.8'

services:
  mercure:
    image: caddy:latest
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - ./data:/var/lib/mercure
    environment:
      MERCURE_JWT_KEY: my-secret-key
      MERCURE_JWT_ALG: HS256
```

```caddyfile
# Caddyfile
{$DOMAIN:localhost}

mercure {
  anonymous
  demo
  ui

  transport bolt {
    path /var/lib/mercure/hub.db
    size 10000
    cleanup_frequency 0.3
  }

  publisher_jwt {$MERCURE_JWT_KEY} {$MERCURE_JWT_ALG}
  subscriber_jwt {$MERCURE_JWT_KEY} {$MERCURE_JWT_ALG}
}

encode gzip
```

### Redis Standalone

```yaml
# docker-compose.redis.yml
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data
    command: redis-server --appendonly yes

  mercure:
    image: caddy:latest
    ports:
      - "80:80"
      - "443:443"
    depends_on:
      - redis
    volumes:
      - ./Caddyfile.redis:/etc/caddy/Caddyfile:ro
    environment:
      MERCURE_JWT_KEY: my-secret-key
      MERCURE_JWT_ALG: HS256
      REDIS_HOST: redis
      REDIS_PORT: 6379

volumes:
  redis_data:
```

```caddyfile
# Caddyfile.redis
{$DOMAIN:localhost}

mercure {
  anonymous
  demo
  ui

  transport redis {
    addresses {$REDIS_HOST}:{$REDIS_PORT}
    size 50000
    cleanup_frequency 0.3
  }

  publisher_jwt {$MERCURE_JWT_KEY} {$MERCURE_JWT_ALG}
  subscriber_jwt {$MERCURE_JWT_KEY} {$MERCURE_JWT_ALG}
}

encode gzip
```

### Redis Cluster

```yaml
# docker-compose.cluster.yml
version: '3.8'

services:
  redis-node-1:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    command: redis-server --cluster-enabled yes --cluster-config-file nodes-1.conf --port 6379
    volumes:
      - redis_node_1:/data

  redis-node-2:
    image: redis:7-alpine
    ports:
      - "6380:6380"
    command: redis-server --cluster-enabled yes --cluster-config-file nodes-2.conf --port 6380
    volumes:
      - redis_node_2:/data

  redis-node-3:
    image: redis:7-alpine
    ports:
      - "6381:6381"
    command: redis-server --cluster-enabled yes --cluster-config-file nodes-3.conf --port 6381
    volumes:
      - redis_node_3:/data

  mercure:
    image: caddy:latest
    ports:
      - "80:80"
      - "443:443"
    depends_on:
      - redis-node-1
      - redis-node-2
      - redis-node-3
    volumes:
      - ./Caddyfile.cluster:/etc/caddy/Caddyfile:ro
    environment:
      MERCURE_JWT_KEY: my-secret-key
      MERCURE_JWT_ALG: HS256

volumes:
  redis_node_1:
  redis_node_2:
  redis_node_3:
```

```caddyfile
# Caddyfile.cluster
{$DOMAIN:localhost}

mercure {
  anonymous
  demo
  ui

  transport redis {
    addresses redis-node-1:6379 redis-node-2:6380 redis-node-3:6381
    size 100000
    cleanup_frequency 0.5
  }

  publisher_jwt {$MERCURE_JWT_KEY} {$MERCURE_JWT_ALG}
  subscriber_jwt {$MERCURE_JWT_KEY} {$MERCURE_JWT_ALG}
}

encode gzip
```

---

## Migration Guide

### From Bolt to Redis

1. **Deploy Redis** (standalone or cluster)
2. **Update Mercure configuration** to use Redis transport
3. **Restart Mercure** - historical events from Bolt will not transfer
4. **Optionally keep Bolt** as a backup during transition period

### Configuration Switch Example

```caddyfile
# Before (Bolt)
transport bolt {
  path /var/lib/mercure/hub.db
}

# After (Redis)
transport redis {
  addresses redis:6379
  password ${REDIS_PASSWORD}
}
```

---

## Performance Tuning

### Cleanup Frequency

- **0.3** (default): Cleanup on 30% of operations
- **1.0**: Cleanup on every operation (slower but more memory-efficient)
- **0.1**: Cleanup on 10% of operations (faster but uses more memory)

### Size Limits

| Size | Best For | Memory (Redis) | Memory (Bolt) |
|------|----------|-----------------|--------------|
| 10,000 | Development | ~20MB | ~5MB |
| 50,000 | Medium traffic | ~100MB | ~20MB |
| 100,000 | High traffic | ~200MB | ~40MB |
| 500,000 | Very high traffic | ~1GB | ~200MB |

---

## Health Checks

Both transports implement the `TransportHealthChecker` interface for Kubernetes probes:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 80
  initialDelaySeconds: 10
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 80
  initialDelaySeconds: 5
  periodSeconds: 5
```

---

## Monitoring

### Prometheus Metrics (Available for both transports)

- `mercure_updates_published_total`: Total updates published
- `mercure_subscribers_total`: Current subscriber count
- `mercure_dispatch_duration_seconds`: Update dispatch duration

### Redis-Specific Monitoring

```bash
# Monitor Redis usage
redis-cli INFO stats
redis-cli INFO memory
redis-cli DBSIZE

# Monitor Mercure keys
redis-cli KEYS "mercure:*"
```

---

## Troubleshooting

### Redis Connection Issues

```
Error: unable to connect to Redis: connection refused
```

**Solution**: Check Redis is running and accessible
```bash
redis-cli ping
```

### Cluster Slot Errors

```
Error: CLUSTERDOWN Hash slot not served
```

**Solution**: Ensure all cluster nodes are properly initialized
```bash
redis-cli --cluster check localhost:6379
```

### High Memory Usage

**Solution**: 
1. Reduce `size` parameter
2. Increase `cleanup_frequency` (closer to 1.0)
3. Enable Redis key expiration

---

## Testing Transport

```bash
# Test connection
curl -v http://localhost/.well-known/mercure

# Test Redis connection
docker exec mercure redis-cli ping
# Expected: PONG

# Check stored updates
docker exec mercure redis-cli KEYS "mercure:events:*"
```

---

## Production Recommendations

### For Bolt

- Use on single-server deployments only
- Enable regular backups of the database file
- Monitor disk space
- Set reasonable `size` limits

### For Redis Standalone

- Use Sentinel for automatic failover
- Enable AOF persistence
- Set up monitoring and alerts
- Use password authentication
- Implement regular backups

### For Redis Cluster

- Minimum 3 master nodes recommended
- Use node affinity in Kubernetes
- Enable persistence on all nodes
- Monitor cluster health
- Plan capacity based on subscriber count
- Use managed Redis services (AWS ElastiCache, Google Cloud Memorystore) when possible

---

## Environment Variable Support

All configuration supports environment variable substitution:

```caddyfile
transport redis {
  addresses {$REDIS_HOST}:{$REDIS_PORT}
  password {$REDIS_PASSWORD}
  key_prefix {$REDIS_KEY_PREFIX:mercure:}
  size {$REDIS_SIZE:50000}
}
```

Usage:
```bash
REDIS_HOST=redis.example.com \
REDIS_PORT=6379 \
REDIS_PASSWORD=secret \
caddy run --config Caddyfile
```

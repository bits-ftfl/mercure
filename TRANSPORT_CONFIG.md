# Transport Configuration Guide

This guide demonstrates how to configure Mercure with different transport backends: **Bolt** (embedded database) or **Redis** (distributed systems).

## Bolt Transport (Default)

Bolt is an embedded key-value database, ideal for single-instance deployments.

### JSON Configuration

```json
{
  "http": {
    "servers": {
      "main": {
        "listen": [":8080"],
        "routes": [
          {
            "handle": [
              {
                "handler": "mercure",
                "transport": {
                  "name": "bolt",
                  "path": "/data/mercure.db",
                  "bucket_name": "updates",
                  "size": 10000,
                  "cleanup_frequency": 0.3
                }
              }
            ]
          }
        ]
      }
    }
  }
}
```

### Caddyfile Configuration

```caddyfile
{
  log default {
    level debug
  }
}

:8080 {
  mercure {
    publisher_jwt {shared_key} HS256
    subscriber_jwt {shared_key} HS256
    anonymous
    demo
    ui

    transport bolt {
      path /data/mercure.db
      bucket_name updates
      size 10000
      cleanup_frequency 0.3
    }
  }
}
```

### Bolt Options

- **path**: Directory path for the Bolt database file (default: `bolt.db`)
- **bucket_name**: Name of the Bolt bucket to store updates (default: `updates`)
- **size**: Maximum number of updates to keep in history (0 = unlimited)
- **cleanup_frequency**: Probability of cleanup on each update, 0-1 (default: 0.3)

---

## Redis Transport (Standalone)

Redis standalone mode is suitable for deployments with a single Redis instance.

### JSON Configuration

```json
{
  "http": {
    "servers": {
      "main": {
        "listen": [":8080"],
        "routes": [
          {
            "handle": [
              {
                "handler": "mercure",
                "transport": {
                  "name": "redis",
                  "addresses": ["redis.example.com:6379"],
                  "password": "your-password",
                  "db": 0,
                  "key_prefix": "mercure:",
                  "channel_prefix": "mercure:updates",
                  "size": 10000,
                  "cleanup_frequency": 0.3
                }
              }
            ]
          }
        ]
      }
    }
  }
}
```

### Caddyfile Configuration

```caddyfile
{
  log default {
    level debug
  }
}

:8080 {
  mercure {
    publisher_jwt {shared_key} HS256
    subscriber_jwt {shared_key} HS256
    anonymous
    demo
    ui

    transport redis {
      addresses redis.example.com:6379
      password your-password
      db 0
      key_prefix mercure:
      channel_prefix mercure:updates
      size 10000
      cleanup_frequency 0.3
    }
  }
}
```

### Redis Options

- **addresses**: Redis server address(es) in format `host:port` (default: `localhost:6379`)
- **password**: Authentication password for Redis (optional)
- **db**: Database number for standalone mode (default: 0, ignored in cluster mode)
- **key_prefix**: Prefix for all Redis keys (default: `mercure:`)
- **channel_prefix**: Prefix for Pub/Sub channel names (default: `mercure:updates`)
- **size**: Maximum number of updates to keep in history (0 = unlimited)
- **cleanup_frequency**: Probability of cleanup on each update, 0-1 (default: 0.3)

---

## Redis Transport (Cluster Mode)

Redis Cluster is ideal for high-availability and scalability requirements.

### JSON Configuration

```json
{
  "http": {
    "servers": {
      "main": {
        "listen": [":8080"],
        "routes": [
          {
            "handle": [
              {
                "handler": "mercure",
                "transport": {
                  "name": "redis",
                  "addresses": [
                    "redis-node1.example.com:6379",
                    "redis-node2.example.com:6379",
                    "redis-node3.example.com:6379"
                  ],
                  "password": "cluster-password",
                  "key_prefix": "mercure:",
                  "channel_prefix": "mercure:updates",
                  "size": 50000,
                  "cleanup_frequency": 0.5
                }
              }
            ]
          }
        ]
      }
    }
  }
}
```

### Caddyfile Configuration

```caddyfile
{
  log default {
    level debug
  }
}

:8080 {
  mercure {
    publisher_jwt {shared_key} HS256
    subscriber_jwt {shared_key} HS256
    anonymous
    demo
    ui

    transport redis {
      addresses redis-node1.example.com:6379 redis-node2.example.com:6379 redis-node3.example.com:6379
      password cluster-password
      key_prefix mercure:
      channel_prefix mercure:updates
      size 50000
      cleanup_frequency 0.5
    }
  }
}
```

---

## Comparison

| Feature | Bolt | Redis Standalone | Redis Cluster |
|---------|------|------------------|---------------|
| **Setup Complexity** | Simple | Moderate | Complex |
| **Single Instance** | ✅ Best | ✅ Good | ❌ Not recommended |
| **High Availability** | ❌ No | ⚠️ Limited | ✅ Yes |
| **Scalability** | Limited | Good | ✅ Excellent |
| **Distributed Mode** | ❌ No | ✅ Yes (with Pub/Sub) | ✅ Yes (with Pub/Sub) |
| **Data Persistence** | ✅ Automatic | ✅ With RDB/AOF | ✅ With RDB/AOF |
| **Memory Usage** | Local storage | Shared | Distributed |

---

## Migration from Bolt to Redis

When migrating from Bolt to Redis:

1. **Stop the Mercure instance**
2. **Update the transport configuration** to use Redis
3. **Start the new instance** - Redis history will start fresh
4. **Update all client connections** to point to the new Redis-backed instance

Note: History data from Bolt won't automatically transfer to Redis. Plan data retention accordingly.

---

## Performance Tuning

### For Bolt
```caddyfile
transport bolt {
  path /ssd/mercure.db  # Use fast storage
  size 5000             # Smaller size = faster cleanup
  cleanup_frequency 1.0 # More aggressive cleanup
}
```

### For Redis
```caddyfile
transport redis {
  addresses redis.example.com:6379
  size 100000           # Larger history buffer
  cleanup_frequency 0.1 # Less frequent cleanup
}
```

---

## Monitoring

### Bolt
- Monitor disk space and I/O operations
- Check file size of `mercure.db`

### Redis
- Use `redis-cli INFO` to monitor memory usage
- Monitor Pub/Sub subscriber count
- Check keyspace statistics

---

## Docker Examples

### Bolt with Docker

```yaml
version: '3'
services:
  mercure:
    image: dunglas/mercure
    ports:
      - "80:80"
    environment:
      SERVER_NAME: example.com
      PUBLISHER_JWT_KEY: my-secret-key
      SUBSCRIBER_JWT_KEY: my-secret-key
      MERCURE_EXTRA_DIRECTIVES: |
        mercure {
          transport bolt {
            path /data/mercure.db
            size 10000
          }
        }
    volumes:
      - mercure_data:/data

volumes:
  mercure_data:
```

### Redis Cluster with Docker

```yaml
version: '3'
services:
  redis-node1:
    image: redis:latest
    command: redis-server --port 6379 --cluster-enabled yes

  redis-node2:
    image: redis:latest
    command: redis-server --port 6380 --cluster-enabled yes

  redis-node3:
    image: redis:latest
    command: redis-server --port 6381 --cluster-enabled yes

  mercure:
    image: dunglas/mercure
    ports:
      - "80:80"
    environment:
      SERVER_NAME: example.com
      PUBLISHER_JWT_KEY: my-secret-key
      SUBSCRIBER_JWT_KEY: my-secret-key
      MERCURE_EXTRA_DIRECTIVES: |
        mercure {
          transport redis {
            addresses redis-node1:6379 redis-node2:6380 redis-node3:6381
            size 50000
          }
        }
    depends_on:
      - redis-node1
      - redis-node2
      - redis-node3
```

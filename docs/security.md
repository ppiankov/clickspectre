# Security & Safety

clickspectre is **strictly read-only**. It does not modify anything in ClickHouse or Kubernetes.

## What it does

- Runs only `SELECT` queries against ClickHouse (`system.query_log`, metadata tables)
- Reads Kubernetes metadata when `--resolve-k8s` is used
- Builds a static usage report you can review
- Makes non-destructive recommendations about unused tables

## What it NEVER does

- No `DROP TABLE`, `ALTER TABLE`, `INSERT`, `UPDATE`, or `DELETE`
- No schema changes of any kind
- No writes to your ClickHouse cluster
- No modifications to Kubernetes resources (no deletes, no patches, no apply)

## ClickHouse protection

- Query timeouts (when using non-readonly users)
- Batch processing (default: 100k rows/batch)
- Max rows limit (default: 1M rows)
- Connection pooling (max 10 connections)
- Self-exclusion (skips system.query_log queries)
- Readonly users work but cannot use query timeouts

## Kubernetes protection

- In-memory cache (5 min TTL)
- Rate limiting (default: 10 RPS)
- Request timeouts (5s per request)
- Graceful fallback to raw IPs
- Read-only API calls: Pod/IP mappings, Service metadata, Namespace info

## Cleanup recommendations

Conservative scoring that:
- Never recommends system tables
- Never recommends tables with writes in last 7 days
- Never recommends materialized views or MV dependencies
- Flags anomalous tables as "suspect" not "safe"
- Separates zero-usage tables by replication status
- Applies size filtering to focus on meaningful cleanup

## Readonly user support

```bash
# Works with readonly users
clickspectre analyze \
  --clickhouse-dsn "clickhouse://readonly:password@host:9000/database" \
  --lookback 7d \
  --batch-size 1000 \
  --max-rows 50000
```

**Limitations:** No query timeout protection. Use smaller batch sizes and shorter lookback periods.

**Recommended:** Create a dedicated non-readonly user with SELECT-only permissions:

```sql
CREATE USER clickspectre IDENTIFIED BY 'your-password';
GRANT SELECT ON *.* TO clickspectre;
```

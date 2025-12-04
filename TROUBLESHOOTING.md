# ClickSpectre Troubleshooting Guide

## Common Issues and Solutions

### 1. "Cannot modify 'max_execution_time' setting in readonly mode"

**Error:**
```
code: 164, message: Cannot modify 'max_execution_time' setting in readonly mode
```

**Cause:** The ClickHouse user has readonly permissions and cannot modify query settings.

**Solutions:**

#### Option A: Use a Non-Readonly User (Recommended)
Create or use a ClickHouse user with standard (non-readonly) permissions:

```sql
-- Create a dedicated analysis user
CREATE USER clickspectre IDENTIFIED BY 'your-password';

-- Grant read-only access to tables
GRANT SELECT ON *.* TO clickspectre;

-- Allow the user to modify their own session settings
GRANT ALTER SETTINGS ON *.* TO clickspectre;
```

Then use:
```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://clickspectre:password@host:9000/database"
```

#### Option B: Modify the DSN to Skip Settings
We can update the collector to avoid modifying settings for readonly users. The ClickHouse Go driver allows skipping settings via connection options.

**For now**, if you can't change the user, you can:
1. Use a different user account that's not in readonly mode
2. Ask your ClickHouse admin to grant `ALTER SETTINGS` permission
3. Use a service account with appropriate permissions

---

### 2. Duration Format Issues

**Problem:** "unknown unit 'd' in duration"

**Solution:** ✅ **FIXED** - Now supports day format!

You can now use:
- `7d` = 7 days
- `30d` = 30 days (default)
- `90d` = 90 days

Or traditional Go format:
- `168h` = 7 days
- `720h` = 30 days
- `5m` = 5 minutes
- `1h` = 1 hour

---

### 3. Connection Refused

**Error:**
```
failed to ping ClickHouse: dial tcp: connection refused
```

**Solutions:**
- Verify ClickHouse is running: `clickhouse-client --query "SELECT 1"`
- Check the host and port are correct
- Verify network connectivity: `telnet host 9000`
- Check firewall rules
- Try using the IP address instead of hostname

---

### 4. Authentication Failed

**Error:**
```
authentication failed
```

**Solutions:**
- Verify username and password
- Check DSN format: `clickhouse://user:password@host:port/database`
- Special characters in password? URL-encode them
- Try connecting with `clickhouse-client` to verify credentials

---

### 5. "Table system.query_log doesn't exist"

**Error:**
```
code: 60, message: Table system.query_log doesn't exist
```

**Solutions:**
- ClickHouse query logging may be disabled
- Enable query logging in `config.xml`:
  ```xml
  <query_log>
      <database>system</database>
      <table>query_log</table>
  </query_log>
  ```
- Restart ClickHouse
- Check if query_log table exists: `SELECT count() FROM system.query_log`

---

### 6. Out of Memory / Query Too Large

**Error:**
```
Memory limit exceeded
```

**Solutions:**
- Reduce lookback period: `--lookback 7d`
- Reduce batch size: `--batch-size 50000`
- Reduce max rows: `--max-rows 500000`
- Increase query timeout: `--query-timeout 10m`

---

### 7. Kubernetes Resolution Fails

**Error:**
```
Failed to initialize K8s resolver
```

**Solutions:**
- Check kubeconfig exists: `ls ~/.kube/config`
- Verify cluster access: `kubectl cluster-info`
- Try specifying kubeconfig explicitly: `--kubeconfig /path/to/config`
- Disable K8s resolution: `--resolve-k8s=false`

---

### 8. Report Generation Issues

**Error:**
```
web directory not found
```

**Solutions:**
- Run clickspectre from the project root directory
- Or copy the `web/` directory to where you're running from
- Future: We'll add embedded assets to avoid this

---

## Debugging Tips

### Enable Verbose Logging
```bash
clickspectre analyze \
  --clickhouse-dsn "..." \
  --verbose
```

### Dry Run Mode
Test without writing output:
```bash
clickspectre analyze \
  --clickhouse-dsn "..." \
  --dry-run \
  --verbose
```

### Test Connection
```bash
# Test ClickHouse connection
clickhouse-client --host your-host --port 9000 --user your-user --password your-password --query "SELECT 1"

# Check query_log exists
clickhouse-client --query "SELECT count() FROM system.query_log LIMIT 1"

# Test Kubernetes access
kubectl get pods
```

---

## Performance Tuning

### For Large Datasets (> 1M queries)

```bash
clickspectre analyze \
  --clickhouse-dsn "..." \
  --lookback 30d \
  --batch-size 50000 \
  --max-rows 2000000 \
  --concurrency 10 \
  --query-timeout 10m
```

### For Small Datasets (< 100K queries)

```bash
clickspectre analyze \
  --clickhouse-dsn "..." \
  --lookback 7d \
  --batch-size 100000 \
  --concurrency 3
```

---

## Getting Help

If you encounter issues not covered here:

1. Check logs with `--verbose` flag
2. Try `--dry-run` to test without side effects
3. Verify ClickHouse and Kubernetes connectivity separately
4. Open an issue: https://github.com/ppiankov/clickspectre/issues

---

## Common DSN Formats

```bash
# Standard TCP
clickhouse://user:password@host:9000/database

# HTTP (not recommended)
http://user:password@host:8123/database

# With special characters in password (URL-encode)
clickhouse://user:p%40ssw0rd@host:9000/database

# With query parameters
clickhouse://user:password@host:9000/database?dial_timeout=10s

# Multiple hosts (failover)
clickhouse://user:password@host1:9000,host2:9000/database
```

---

## Quick Diagnostics

Run this script to diagnose common issues:

```bash
#!/bin/bash
echo "=== ClickSpectre Diagnostics ==="
echo ""
echo "1. Binary version:"
./bin/clickspectre version
echo ""
echo "2. ClickHouse connection test:"
clickhouse-client --host your-host --port 9000 --query "SELECT 1" || echo "FAILED"
echo ""
echo "3. Query log check:"
clickhouse-client --host your-host --port 9000 --query "SELECT count() FROM system.query_log LIMIT 1" || echo "FAILED"
echo ""
echo "4. Kubernetes access:"
kubectl cluster-info || echo "FAILED (OK if not using K8s)"
echo ""
echo "5. Web assets check:"
ls -la web/ || echo "FAILED - web directory not found"
```

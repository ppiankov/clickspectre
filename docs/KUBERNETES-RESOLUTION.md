# Kubernetes IP Resolution

## What Does K8s Resolution Do?

Kubernetes resolution (`--resolve-k8s`) enriches your ClickHouse query log analysis by **resolving client IP addresses to Kubernetes service and pod names**.

### Without K8s Resolution

Services are identified only by IP address:

```json
{
  "ip": "10.0.1.100",
  "k8s_service": "10.0.1.100",  // Just the IP
  "k8s_namespace": "",
  "k8s_pod": "",
  "tables_used": ["users", "orders"],
  "query_count": 1000
}
```

**In the report UI:** You see raw IPs like `10.0.1.100` which are hard to identify.

### With K8s Resolution

Services are identified by their Kubernetes names:

```json
{
  "ip": "10.0.1.100",
  "k8s_service": "api-server",      // Resolved!
  "k8s_namespace": "production",    // Namespace
  "k8s_pod": "api-server-abc123",   // Pod name
  "tables_used": ["users", "orders"],
  "query_count": 1000
}
```

**In the report UI:** You see meaningful names like `production/api-server` instead of IPs.

---

## How It Works

```
ClickHouse Query Log              Kubernetes API
┌─────────────────┐              ┌──────────────────┐
│ client_ip       │              │ Pod              │
│ 10.0.1.100    │─────────────>│ IP: 10.0.1.100 │
└─────────────────┘              │ Name: api-server │
                                 │ NS: production   │
                                 │ Service: api-svc │
                                 └──────────────────┘
```

**Steps:**
1. ClickSpectre reads `initial_address` (client IP) from `system.query_log`
2. Strips IPv6-mapped prefix (`::ffff:` → clean IPv4)
3. Queries Kubernetes API: `kubectl get pods --all-namespaces --field-selector status.podIP=<IP>`
4. Finds the pod and its owning service
5. Caches the result (5 min TTL by default)
6. Enriches the report with K8s metadata

---

## Requirements

To use K8s resolution, you need:

### 1. Kubernetes Access

```bash
# Test your kubectl access
kubectl get pods --all-namespaces
```

If this works, ClickSpectre can resolve IPs.

### 2. Kubeconfig

```bash
# Default location
~/.kube/config

# Or specify custom path
clickspectre analyze --resolve-k8s --kubeconfig /path/to/config
```

### 3. RBAC Permissions

Your kubeconfig user needs at least:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: clickspectre-reader
rules:
- apiGroups: [""]
  resources: ["pods", "services"]
  verbs: ["get", "list"]
```

---

## Usage

### Enable K8s Resolution

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --output ./report \
  --resolve-k8s \
  --verbose
```

### With Custom Kubeconfig

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --resolve-k8s \
  --kubeconfig ~/.kube/production-config \
  --verbose
```

### Adjust Rate Limiting

```bash
# If you hit K8s API rate limits, reduce RPS
clickspectre analyze \
  --resolve-k8s \
  --k8s-rate-limit 5 \
  --k8s-cache-ttl 10m
```

---

## Verification

### Check if K8s Resolution Worked

```bash
# After analysis, check the report
cat ./report/report.json | jq '.metadata.k8s_resolution_enabled'
# Should output: true

# Check if services have resolved names
cat ./report/report.json | jq '.services[0:3] | .[] | {ip, k8s_service, k8s_namespace}'
```

**Expected output (working):**
```json
{
  "ip": "::ffff:10.0.1.100",
  "k8s_service": "api-server",
  "k8s_namespace": "production"
}
```

**If NOT working (fallback to IP):**
```json
{
  "ip": "::ffff:10.0.1.100",
  "k8s_service": "::ffff:10.0.1.100",  // Still just the IP
  "k8s_namespace": ""
}
```

### Verbose Logging

Run with `--verbose` to see resolution details:

```bash
clickspectre analyze --resolve-k8s --verbose
```

**Look for these log messages:**

```
✅ Success:
Stripped IPv6-mapped prefix: ::ffff:10.0.1.100 → 10.0.1.100
Resolved IP 10.0.1.100 → production/api-server (pod: api-server-abc123)

❌ Failure:
Failed to resolve IP 10.0.1.100: no pod found with IP 10.0.1.100 (falling back to raw IP)
```

---

## Common Issues

### 1. All Services Show IPs Instead of Names

**Symptom:**
```json
{"k8s_service": "::ffff:10.0.1.100"}
```

**Causes:**
- Kubeconfig not found or invalid
- No access to Kubernetes cluster
- IPs don't match any pods (queries from outside K8s)
- IPv6 mapping issue (fixed in latest version)

**Fix:**
```bash
# Test kubectl access
kubectl get pods --all-namespaces

# Try with explicit kubeconfig
clickspectre analyze --resolve-k8s --kubeconfig ~/.kube/config --verbose
```

### 2. Some IPs Resolved, Some Not

**This is normal!** IPs from:
- `127.0.0.1` - localhost (ClickHouse internal queries)
- `0.0.0.0` - System queries
- External IPs - Queries from outside the cluster

These **cannot** be resolved to K8s services and will fall back to showing the IP.

### 3. K8s API Rate Limiting

**Symptom:**
```
rate limiter wait failed: context canceled
```

**Fix:**
```bash
# Reduce rate limit
clickspectre analyze --resolve-k8s --k8s-rate-limit 5

# Increase cache TTL to reduce lookups
clickspectre analyze --resolve-k8s --k8s-cache-ttl 15m
```

### 4. Permission Denied

**Symptom:**
```
failed to list pods: pods is forbidden: User "system:serviceaccount:default:default" cannot list resource "pods" in API group ""
```

**Fix:** Grant RBAC permissions (see Requirements section above)

---

## Performance Impact

K8s resolution adds minimal overhead:

| Configuration | Impact |
|--------------|---------|
| **Default** (10 RPS, 5 min cache) | ~1-2 seconds for 100 unique IPs |
| **Cached** (repeated IPs) | ~0ms (instant cache hits) |
| **High rate** (50 RPS, 1 min cache) | Faster but may hit K8s API limits |

**Best practices:**
- Use default rate limit (10 RPS) for most cases
- Increase cache TTL (10-15 min) for large datasets
- Reduce rate limit if you see K8s API errors

---

## Benefits of K8s Resolution

### 1. **Better Service Identification**

Instead of: "IP 10.0.1.100 uses table X"
You get: "production/api-server uses table X"

### 2. **Easier Troubleshooting**

Find which service is causing issues:
```bash
# Without K8s: "Heavy query from 10.0.1.100"
# With K8s:    "Heavy query from production/data-processor"
```

### 3. **Namespace Awareness**

See which namespace accesses which tables:
- `production/api-server` → `users`, `orders`
- `staging/api-server` → `users_staging`
- `monitoring/prometheus` → system tables

### 4. **Better Visualizations**

The D3.js bipartite graph shows:
- **Nodes**: Kubernetes service names (not IPs)
- **Edges**: Service → Table connections
- **Colors**: Namespace-based coloring

---

## Disabling K8s Resolution

If you don't need K8s service names, **omit the flag** for faster analysis:

```bash
# Without K8s resolution (faster)
clickspectre analyze --clickhouse-dsn "..." --output ./report
```

This is useful when:
- ClickHouse is not accessed from Kubernetes
- You only care about table usage, not which service uses them
- Faster analysis is needed
- No kubectl access available

---

## Examples

### Example 1: Production Cluster Analysis

```bash
clickspectre analyze \
  --clickhouse-dsn "clickhouse://prod:9000/default" \
  --output ./prod-report \
  --lookback 30d \
  --resolve-k8s \
  --kubeconfig ~/.kube/prod-config \
  --k8s-rate-limit 20 \
  --verbose
```

### Example 2: Multi-Cluster

```bash
# Cluster 1
clickspectre analyze \
  --clickhouse-dsn "$CLUSTER1_DSN" \
  --output ./reports/cluster1 \
  --resolve-k8s \
  --kubeconfig ~/.kube/cluster1

# Cluster 2
clickspectre analyze \
  --clickhouse-dsn "$CLUSTER2_DSN" \
  --output ./reports/cluster2 \
  --resolve-k8s \
  --kubeconfig ~/.kube/cluster2
```

### Example 3: Without K8s (Faster)

```bash
# When you don't need service names
clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --output ./report \
  --lookback 90d
```

---

## Technical Details

### Caching Strategy

- **Cache TTL**: 5 minutes (configurable)
- **Cache size**: Unlimited (LRU eviction could be added)
- **Cache hit rate**: Typically >95% for repeated IPs

### Rate Limiting

- **Algorithm**: Token bucket
- **Default**: 10 requests/second
- **Burst**: 20 requests
- **Purpose**: Protect Kubernetes API from overload

### Fallback Behavior

When K8s resolution fails:
1. Log the error (if `--verbose`)
2. Use raw IP as service name
3. Cache the fallback result
4. Continue analysis (no failure)

This ensures **graceful degradation** - analysis never fails due to K8s issues.

---

## FAQ

**Q: Do I need K8s resolution?**
A: Only if your ClickHouse is accessed from Kubernetes pods and you want to see service names instead of IPs.

**Q: Will analysis fail if kubectl doesn't work?**
A: No! It falls back to showing IPs. Analysis continues normally.

**Q: Can I use K8s resolution with readonly ClickHouse users?**
A: Yes! K8s resolution is independent of ClickHouse permissions.

**Q: Does it work with multiple Kubernetes clusters?**
A: Yes, but you need to run separate analyses with different `--kubeconfig` files.

**Q: What if some IPs are external (not in K8s)?**
A: Those IPs will show as-is. Only IPs matching K8s pods are resolved.

---

## Troubleshooting Script

```bash
#!/bin/bash
# Test K8s resolution
echo "=== ClickSpectre K8s Resolution Test ==="
echo ""

echo "1. Testing kubectl access..."
kubectl get pods --all-namespaces | head -5 || echo "FAILED: kubectl not working"
echo ""

echo "2. Running analysis with K8s resolution..."
./bin/clickspectre analyze \
  --clickhouse-dsn "$CLICKHOUSE_DSN" \
  --output /tmp/k8s-test \
  --lookback 1h \
  --max-rows 1000 \
  --resolve-k8s \
  --verbose | grep -E "(Resolved|Failed to resolve|Stripped)"
echo ""

echo "3. Checking results..."
cat /tmp/k8s-test/report.json | jq '.services[0:3] | .[] | {ip, k8s_service, k8s_namespace}'
echo ""

echo "4. Summary:"
K8S_ENABLED=$(cat /tmp/k8s-test/report.json | jq '.metadata.k8s_resolution_enabled')
echo "   K8s resolution enabled: $K8S_ENABLED"

RESOLVED_COUNT=$(cat /tmp/k8s-test/report.json | jq '[.services[] | select(.k8s_namespace != "")] | length')
TOTAL_COUNT=$(cat /tmp/k8s-test/report.json | jq '.services | length')
echo "   Resolved services: $RESOLVED_COUNT / $TOTAL_COUNT"
```

Save as `test-k8s-resolution.sh` and run to diagnose issues.

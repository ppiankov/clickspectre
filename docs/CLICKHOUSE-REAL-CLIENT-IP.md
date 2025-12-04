# Configuring ClickHouse to Log Real Client IPs Behind Load Balancers

## Problem

When ClickHouse sits behind a load balancer, `system.query_log` records the **load balancer IP** instead of the real client IP:

```
Load Balancer IP: 10.0.1.100  ← This is what ClickHouse sees
Real Client IP:   10.0.10.56  ← This is what we need
```

**Result:** K8s resolution fails because LB IPs don't match any K8s pods.

## Solutions

### Option 1: PROXY Protocol (Best for Native TCP Protocol)

**When to use:** Clients connect via native ClickHouse protocol (port 9000)

**How it works:** Load balancer prepends client IP information to the TCP stream

#### 1.1. Configure ClickHouse

Edit `/etc/clickhouse-server/config.xml`:

```xml
<clickhouse>
    <!-- Standard ports -->
    <tcp_port>9000</tcp_port>
    <http_port>8123</http_port>

    <!-- Enable PROXY protocol on these ports -->
    <tcp_port_proxy>9000</tcp_port_proxy>
    <http_port_proxy>8123</http_port_proxy>

    <!-- Alternatively, use separate ports for proxy protocol -->
    <!-- <tcp_port_proxy>9001</tcp_port_proxy> -->

    <listen_host>::</listen_host>
</clickhouse>
```

**Restart ClickHouse:**
```bash
sudo systemctl restart clickhouse-server
```

#### 1.2. Configure Load Balancer

**AWS Network Load Balancer (NLB):**
```bash
# Enable proxy protocol v2 on target group
aws elbv2 modify-target-group-attributes \
  --target-group-arn arn:aws:elasticloadbalancing:region:account:targetgroup/clickhouse-tg/xxxx \
  --attributes Key=proxy_protocol_v2.enabled,Value=true
```

**HAProxy:**
```
frontend clickhouse_front
    bind *:9000
    mode tcp
    default_backend clickhouse_back

backend clickhouse_back
    mode tcp
    balance roundrobin
    server ch1 10.0.10.1:9000 send-proxy-v2
    server ch2 10.0.10.2:9000 send-proxy-v2
```

**nginx (TCP Stream Module):**
```nginx
stream {
    upstream clickhouse {
        server 10.0.10.1:9000;
        server 10.0.10.2:9000;
    }

    server {
        listen 9000;
        proxy_pass clickhouse;
        proxy_protocol on;  # Enable proxy protocol
    }
}
```

**Verification:**
```bash
# Check query_log after configuration
clickhouse-client --query "
SELECT
    initial_address,
    count() as queries
FROM system.query_log
WHERE event_time >= now() - INTERVAL 10 MINUTE
GROUP BY initial_address
ORDER BY queries DESC
LIMIT 10"

# Should now show real client IPs like 10.0.10.x instead of LB IPs
```

---

### Option 2: X-Forwarded-For Headers (For HTTP Interface)

**When to use:** Clients connect via HTTP interface (port 8123)

**How it works:** Load balancer adds `X-Forwarded-For` header with real client IP

#### 2.1. Configure ClickHouse

Edit `/etc/clickhouse-server/config.xml`:

```xml
<clickhouse>
    <!-- Trust X-Forwarded-For from these load balancer IPs -->
    <remote_servers_custom_ip>
        <network>
            <ip>10.0.1.100</ip>   <!-- LB IP 1 -->
            <ip>10.0.1.200</ip>   <!-- LB IP 2 -->
            <!-- Add all your LB IPs -->
        </network>
    </remote_servers_custom_ip>

    <!-- Or trust entire subnet -->
    <remote_servers_custom_ip>
        <network>
            <ip>10.0.0.0</ip>
            <mask>255.255.0.0</mask>
        </network>
    </remote_servers_custom_ip>
</clickhouse>
```

**Restart ClickHouse:**
```bash
sudo systemctl restart clickhouse-server
```

#### 2.2. Configure Load Balancer

**AWS Application Load Balancer (ALB):**
- ALB automatically adds `X-Forwarded-For` header
- No additional configuration needed

**nginx:**
```nginx
server {
    listen 8123;

    location / {
        proxy_pass http://clickhouse_backend;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header Host $host;
    }
}
```

**HAProxy:**
```
frontend http_front
    bind *:8123
    default_backend clickhouse_http

backend clickhouse_http
    option forwardfor
    server ch1 10.0.10.1:8123
    server ch2 10.0.10.2:8123
```

---

### Option 3: Client Identification via Query Context

**When to use:** Can't modify LB configuration, but can modify client applications

#### 3.1. Use Query Settings

Have clients set a `quota_key` or use custom settings:

```sql
-- Client sets their identity
SET quota_key = 'production/api-server';

-- Or use custom setting
SET custom_key_1 = 'api-server', custom_key_2 = 'production';
```

Then modify ClickSpectre to read `quota_key` instead of IP:

```go
// In analyzer, use quota_key if available
if entry.QuotaKey != "" {
    serviceName = entry.QuotaKey  // e.g., "production/api-server"
}
```

#### 3.2. Use Query Comments

Have clients add identifying comments:

```sql
/* client: production/api-server */
SELECT * FROM users;
```

Parse in ClickSpectre:

```go
// Extract client info from query comments
func extractClientFromQuery(query string) string {
    pattern := regexp.MustCompile(`/\*\s*client:\s*([^\*]+)\s*\*/`)
    if matches := pattern.FindStringSubmatch(query); len(matches) > 1 {
        return strings.TrimSpace(matches[1])
    }
    return ""
}
```

---

### Option 4: Manual IP Mapping File

**When to use:** Can't modify ClickHouse or LB, need quick solution

Create a mapping file `/etc/clickspectre/ip-mapping.yaml`:

```yaml
ip_mappings:
  10.0.1.100:
    service: clickhouse-lb-1
    namespace: infrastructure
    description: Primary ClickHouse load balancer

  10.0.1.200:
    service: clickhouse-lb-2
    namespace: infrastructure
    description: Secondary ClickHouse load balancer
```

Have ClickSpectre read this file and use it instead of K8s API.

---

## Comparison

| Option | Difficulty | Works With | Pros | Cons |
|--------|-----------|------------|------|------|
| **PROXY Protocol** | Medium | Native TCP | ✅ Accurate<br>✅ No client changes | ⚠️ Requires LB support<br>⚠️ ClickHouse restart |
| **X-Forwarded-For** | Easy | HTTP only | ✅ Standard<br>✅ ALB automatic | ⚠️ HTTP interface only<br>⚠️ Must trust LB IPs |
| **Query Context** | Hard | All | ✅ Works everywhere | ❌ All clients must change<br>❌ Requires code updates |
| **Manual Mapping** | Easy | All | ✅ No config changes<br>✅ Quick fix | ❌ Manual maintenance<br>❌ Not real IPs |

---

## Recommended Approach

### If using Native Protocol (port 9000):
→ **Use PROXY Protocol** (Option 1)

### If using HTTP Interface (port 8123):
→ **Use X-Forwarded-For** (Option 2)

### If you can't modify ClickHouse/LB:
→ **Use Manual Mapping** (Option 4) as temporary solution
→ Plan migration to Option 1 or 2

---

## Testing

### 1. Check Current Behavior

```sql
-- What IPs does ClickHouse currently see?
SELECT
    initial_address,
    count() as query_count,
    uniqExact(user) as unique_users
FROM system.query_log
WHERE event_time >= now() - INTERVAL 1 HOUR
  AND type = 'QueryFinish'
GROUP BY initial_address
ORDER BY query_count DESC;
```

**Before fix:** Shows only LB IPs (10.0.1.100, 10.0.1.200)
**After fix:** Shows real client IPs (10.0.10.x, 10.0.20.x)

### 2. Test PROXY Protocol

```bash
# From a client behind LB, run a query
clickhouse-client --host lb.example.com --query "SELECT 1"

# Check if real IP is logged
clickhouse-client --host clickhouse-node --query "
SELECT
    initial_address,
    query,
    event_time
FROM system.query_log
WHERE query LIKE '%SELECT 1%'
  AND event_time >= now() - INTERVAL 1 MINUTE
ORDER BY event_time DESC
LIMIT 1"
```

### 3. Test X-Forwarded-For

```bash
# HTTP request with X-Forwarded-For
curl -H "X-Forwarded-For: 10.0.10.99" \
  "http://clickhouse-lb:8123/?query=SELECT%201"

# Check log
SELECT initial_address FROM system.query_log
WHERE query LIKE '%SELECT 1%'
ORDER BY event_time DESC LIMIT 1;

# Should show 10.0.10.99
```

---

## ClickSpectre Integration

Once ClickHouse logs real client IPs, regenerate the report:

```bash
# Re-run analysis with K8s resolution
./bin/clickspectre analyze \
  --clickhouse-dsn "clickhouse://host:9000/default" \
  --output ./new-report \
  --lookback 24h \
  --resolve-k8s \
  --kubeconfig ~/.kube/config \
  --verbose

# Check if IPs are now resolved
cat ./new-report/report.json | jq '.services[] | {ip, k8s_service, k8s_namespace}'
```

**Expected output:**
```json
{
  "ip": "::ffff:10.0.10.56",
  "k8s_service": "api-server",      ← Resolved!
  "k8s_namespace": "production"
}
```

---

## Troubleshooting

### Issue: Still seeing LB IPs after configuration

**Check:**
1. ClickHouse was restarted: `sudo systemctl status clickhouse-server`
2. Config was applied: `grep proxy_protocol /etc/clickhouse-server/config.xml`
3. LB proxy protocol is enabled
4. Clients are connecting through the LB (not directly)

### Issue: "Connection reset" errors after enabling proxy protocol

**Cause:** Clients connecting without proxy protocol to a proxy-enabled port

**Fix:** Use separate ports:
```xml
<tcp_port>9000</tcp_port>         <!-- No proxy protocol -->
<tcp_port_proxy>9001</tcp_port_proxy>  <!-- With proxy protocol -->
```

Point LB to port 9001, keep port 9000 for direct access.

### Issue: X-Forwarded-For not working

**Check:**
```sql
-- Check if ClickHouse trusts the LB IP
SELECT * FROM system.settings WHERE name = 'remote_servers_custom_ip';

-- Check if header is being sent
SELECT http_user_agent FROM system.query_log
WHERE interface = 'HTTP' LIMIT 1;
```

---

## Security Considerations

### PROXY Protocol
- **Trust boundary:** Only accept proxy protocol from trusted LBs
- Use firewall rules to block direct connections on proxy-enabled ports
- Verify LB source IPs

### X-Forwarded-For
- **Only trust specific IPs:** Configure `remote_servers_custom_ip` carefully
- **Don't trust public-facing headers:** Attackers can spoof X-Forwarded-For
- Use specific IP list, not broad subnets

---

## AWS-Specific Guide

If using AWS infrastructure:

### NLB + PROXY Protocol

```bash
# 1. Create target group with proxy protocol
aws elbv2 create-target-group \
  --name clickhouse-tg \
  --protocol TCP \
  --port 9000 \
  --vpc-id vpc-xxxxx

# 2. Enable proxy protocol v2
aws elbv2 modify-target-group-attributes \
  --target-group-arn <tg-arn> \
  --attributes Key=proxy_protocol_v2.enabled,Value=true

# 3. Register ClickHouse instances
aws elbv2 register-targets \
  --target-group-arn <tg-arn> \
  --targets Id=i-xxxxx Id=i-yyyyy
```

### ALB + X-Forwarded-For

ALB automatically adds X-Forwarded-For, just configure ClickHouse to trust ALB IPs.

---

## References

- [ClickHouse Proxy Protocol Documentation](https://clickhouse.com/docs/en/interfaces/tcp)
- [AWS NLB Proxy Protocol](https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-target-groups.html#proxy-protocol)
- [HAProxy Proxy Protocol](http://www.haproxy.org/download/2.0/doc/proxy-protocol.txt)
- [nginx Proxy Protocol](http://nginx.org/en/docs/stream/ngx_stream_proxy_module.html#proxy_protocol)

---

## Quick Start

**For most setups (AWS NLB + native protocol):**

1. Enable proxy protocol on ClickHouse:
```xml
<tcp_port_proxy>9000</tcp_port_proxy>
```

2. Enable on AWS:
```bash
aws elbv2 modify-target-group-attributes \
  --target-group-arn <arn> \
  --attributes Key=proxy_protocol_v2.enabled,Value=true
```

3. Restart ClickHouse:
```bash
sudo systemctl restart clickhouse-server
```

4. Test:
```bash
clickhouse-client --query "SELECT initial_address FROM system.query_log ORDER BY event_time DESC LIMIT 5"
```

5. Re-run ClickSpectre:
```bash
./bin/clickspectre analyze --resolve-k8s --verbose
```

Done! Real client IPs will now be logged and K8s resolution will work.

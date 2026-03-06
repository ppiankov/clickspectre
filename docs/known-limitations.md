# Known Limitations

- **Readonly users** — cannot set `max_execution_time` due to ClickHouse permissions. Use smaller batch sizes and shorter lookback periods, or create a non-readonly user with SELECT-only grants
- **Load balancer IPs** — if ClickHouse sits behind a load balancer, client IPs show the LB address instead of real client IPs. Enable PROXY protocol to fix this. See [ClickHouse Real Client IP](CLICKHOUSE-REAL-CLIENT-IP.md)
- **No browser UI** — interactive HTML report is generated as static files, not a live dashboard
- **Single-cluster** — Kubernetes resolution works against one cluster at a time
- **Namespace creation** — `clickspectre deploy` creates the target namespace if it doesn't exist. This is the only write operation on Kubernetes

## Troubleshooting

### "Failed to connect to ClickHouse"

- Check DSN format: `clickhouse://user:password@host:port/database`
- Verify network connectivity
- Check ClickHouse is running: `clickhouse-client --query "SELECT 1"`

### "Cannot modify 'max_execution_time' setting in readonly mode"

Use smaller dataset (`--lookback 7d --batch-size 1000 --max-rows 50000`) or create a non-readonly user with `GRANT SELECT ON *.* TO clickspectre`.

### "Failed to initialize K8s resolver"

- Check kubeconfig path
- Verify cluster connectivity: `kubectl cluster-info`
- Use `--resolve-k8s=false` to disable K8s resolution

### "Query timeout"

- Increase timeout: `--query-timeout 10m`
- Reduce batch size: `--batch-size 50000`
- Reduce max rows: `--max-rows 500000`

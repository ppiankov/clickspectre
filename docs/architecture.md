# Architecture

## Module overview

- **Collector** — queries ClickHouse with pagination and worker pool
- **Analyzer** — processes query logs to build data models
- **Scorer** — evaluates tables for cleanup safety
- **Reporter** — generates JSON+HTML, text, or SARIF outputs
- **K8s Resolver** — (optional) resolves IPs to Kubernetes services

## Report structure

```
report/
├── report.txt         # Human-readable report (--format text)
├── report.sarif       # SARIF output (--format sarif)
├── report.json        # Structured data (--format json)
├── index.html         # Interactive UI (--format json)
├── app.js             # Application logic (--format json)
├── styles.css         # Styling (--format json)
└── libs/
    └── d3.v7.min.js   # D3.js library (--format json)
```

## Report contents

- **Overview** — summary statistics (tables, services, queries, anomalies)
- **Graph** — D3.js bipartite graph visualization (service-to-table relationships)
- **Tables** — sortable table list with usage stats and sparklines
- **Services** — list of services and their table usage
- **Cleanup** — categorized recommendations:
  - Zero Usage Non-Replicated (High Priority)
  - Zero Usage Replicated (Review Carefully)
  - Safe to Drop / Likely Safe / Keep
- **Anomalies** — detected unusual access patterns

## Unused table detection

1. Analyzes `system.query_log` to find which tables were queried
2. Queries `system.tables` to get complete table inventory
3. Compares the two to identify tables with zero usage
4. Separates replicated vs non-replicated tables
5. Filters by size to focus on tables worth cleaning up

## Kubernetes IP resolution

Resolves ClickHouse client IPs to Kubernetes service names. If ClickHouse sits behind a load balancer, enable PROXY protocol to get real client IPs.

See [ClickHouse Real Client IP](CLICKHOUSE-REAL-CLIENT-IP.md) and [Kubernetes Resolution](KUBERNETES-RESOLUTION.md) for setup guides.

## Kubernetes deployment

```bash
# One-command deployment
clickspectre deploy ./my-report --namespace monitoring --port 8080
```

Creates namespace, ConfigMap from report files, nginx deployment, Service, and port-forward.

For custom domain access:

```bash
clickspectre deploy ./my-report --namespace production --ingress-host clickspectre.example.com
```

See [k8s/README.md](../k8s/README.md) for manual deployment and CronJob examples.

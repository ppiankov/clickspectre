// Global data
let report = null;
let currentTab = 'overview';
let tablesSortKey = 'name';
let tablesSortDesc = false;

// Load report on page load
window.addEventListener('DOMContentLoaded', async () => {
    try {
        const response = await fetch('report.json');
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        const contentType = response.headers.get('content-type');
        if (!contentType || !contentType.includes('application/json')) {
            throw new Error(`Expected JSON, got ${contentType}`);
        }
        report = await response.json();
        console.log('Report loaded successfully:', report.metadata);
        renderAll();
    } catch (error) {
        console.error('Failed to load report:', error);
        document.body.innerHTML = `<div class="error">
            <h2>Failed to load report.json</h2>
            <p>${error.message}</p>
            <p>Make sure you're accessing the page via <code>clickspectre serve</code> at <code>http://localhost:8080</code></p>
            <p>Opening the HTML file directly (file://) will not work due to browser security restrictions.</p>
        </div>`;
    }
});

// Render all sections
function renderAll() {
    renderMetadata();
    renderOverview();
    renderTables();
    renderServices();
    renderCleanup();
    renderAnomalies();
    renderGraph();
}

// Render metadata
function renderMetadata() {
    const meta = report.metadata;
    document.getElementById('metadata').innerHTML = `
        <span>Generated: ${new Date(meta.generated_at).toLocaleString()}</span>
        <span>Lookback: ${meta.lookback_days} days</span>
        <span>Host: ${meta.clickhouse_host}</span>
        <span>Duration: ${meta.analysis_duration}</span>
        ${meta.k8s_resolution_enabled ? '<span class="badge">K8s Resolution Enabled</span>' : ''}
    `;
}

// Render overview stat cards
function renderOverview() {
    document.getElementById('total-tables').textContent = report.tables.length;
    document.getElementById('total-services').textContent = report.services.length;
    document.getElementById('total-queries').textContent = report.metadata.total_queries_analyzed.toLocaleString();
    document.getElementById('total-anomalies').textContent = report.anomalies.length;
}

// Render tables
function renderTables() {
    const tbody = document.getElementById('tables-tbody');
    const tables = [...report.tables].sort((a, b) => {
        let aVal = a[tablesSortKey];
        let bVal = b[tablesSortKey];
        if (tablesSortKey === 'last_access') {
            aVal = new Date(aVal);
            bVal = new Date(bVal);
        }
        if (tablesSortDesc) {
            return aVal > bVal ? -1 : 1;
        } else {
            return aVal < bVal ? -1 : 1;
        }
    });

    tbody.innerHTML = tables.map(table => {
        const categoryClass = table.category || 'unknown';
        const sparkline = renderSparkline(table.sparkline);
        return `
            <tr>
                <td><strong>${table.full_name}</strong></td>
                <td>${table.reads.toLocaleString()}</td>
                <td>${table.writes.toLocaleString()}</td>
                <td>${new Date(table.last_access).toLocaleDateString()}</td>
                <td>${sparkline}</td>
                <td>${table.score.toFixed(2)}</td>
                <td><span class="badge badge-${categoryClass}">${table.category}</span></td>
            </tr>
        `;
    }).join('');
}

// Render sparkline (simple text-based for now)
function renderSparkline(data) {
    if (!data || data.length === 0) return '<span class="sparkline">No data</span>';
    const values = data.map(d => d.value);
    const max = Math.max(...values);
    const bars = values.map(v => {
        const height = max > 0 ? Math.floor((v / max) * 10) : 0;
        return '▁▂▃▄▅▆▇█'[Math.min(height, 7)];
    }).join('');
    return `<span class="sparkline">${bars}</span>`;
}

// Sort tables
function sortTables(key) {
    if (tablesSortKey === key) {
        tablesSortDesc = !tablesSortDesc;
    } else {
        tablesSortKey = key;
        tablesSortDesc = false;
    }
    renderTables();
}

// Filter tables
function filterTables() {
    const search = document.getElementById('table-search').value.toLowerCase();
    const rows = document.querySelectorAll('#tables-tbody tr');
    rows.forEach(row => {
        const text = row.textContent.toLowerCase();
        row.style.display = text.includes(search) ? '' : 'none';
    });
}

// Render services
function renderServices() {
    const tbody = document.getElementById('services-tbody');
    tbody.innerHTML = report.services.map(service => {
        const serviceName = service.k8s_service || service.ip;
        const namespace = service.k8s_namespace || '-';
        return `
            <tr>
                <td><strong>${serviceName}</strong></td>
                <td>${namespace}</td>
                <td><code>${service.ip}</code></td>
                <td>${service.tables_used.length}</td>
                <td>${service.query_count.toLocaleString()}</td>
            </tr>
        `;
    }).join('');
}

// Render cleanup recommendations
function renderCleanup() {
    const recs = report.cleanup_recommendations;

    // Render zero-usage tables
    renderZeroUsageNonReplicated();
    renderZeroUsageReplicated();

    // Handle null or empty arrays gracefully
    const safeToDrop = recs.safe_to_drop || [];
    const likelySafe = recs.likely_safe || [];
    const keep = recs.keep || [];

    document.getElementById('safe-to-drop').innerHTML = safeToDrop.length > 0
        ? safeToDrop.map(t => `<li>${t}</li>`).join('')
        : '<li><em>No tables identified</em></li>';

    document.getElementById('likely-safe').innerHTML = likelySafe.length > 0
        ? likelySafe.map(t => `<li>${t}</li>`).join('')
        : '<li><em>No tables identified</em></li>';

    document.getElementById('keep').innerHTML = keep.length > 0
        ? keep.map(t => `<li>${t}</li>`).join('')
        : '<li><em>No tables identified</em></li>';
}

// Render zero-usage non-replicated tables
function renderZeroUsageNonReplicated() {
    const tbody = document.getElementById('zero-usage-non-replicated-tbody');
    const emptyMsg = document.getElementById('zero-usage-non-replicated-empty');
    const table = document.getElementById('zero-usage-non-replicated-table');
    const recs = report.cleanup_recommendations.zero_usage_non_replicated || [];

    if (recs.length === 0) {
        table.style.display = 'none';
        emptyMsg.style.display = 'block';
        return;
    }

    table.style.display = 'table';
    emptyMsg.style.display = 'none';

    tbody.innerHTML = recs.map(t => `
        <tr>
            <td><code>${t.name}</code></td>
            <td><span class="engine-badge">${t.engine}</span></td>
            <td>${t.is_replicated ? '✅ Yes' : '❌ No'}</td>
            <td>${t.size_mb.toFixed(2)}</td>
            <td>${t.rows.toLocaleString()}</td>
        </tr>
    `).join('');
}

// Render zero-usage replicated tables
function renderZeroUsageReplicated() {
    const tbody = document.getElementById('zero-usage-replicated-tbody');
    const emptyMsg = document.getElementById('zero-usage-replicated-empty');
    const table = document.getElementById('zero-usage-replicated-table');
    const recs = report.cleanup_recommendations.zero_usage_replicated || [];

    if (recs.length === 0) {
        table.style.display = 'none';
        emptyMsg.style.display = 'block';
        return;
    }

    table.style.display = 'table';
    emptyMsg.style.display = 'none';

    tbody.innerHTML = recs.map(t => `
        <tr>
            <td><code>${t.name}</code></td>
            <td><span class="engine-badge">${t.engine}</span></td>
            <td>${t.is_replicated ? '✅ Yes' : '❌ No'}</td>
            <td>${t.size_mb.toFixed(2)}</td>
            <td>${t.rows.toLocaleString()}</td>
        </tr>
    `).join('');
}

// Render anomalies
function renderAnomalies() {
    const div = document.getElementById('anomalies-list');
    if (report.anomalies.length === 0) {
        div.innerHTML = '<p class="no-data">No anomalies detected</p>';
        return;
    }

    div.innerHTML = report.anomalies.map(anomaly => {
        const severityClass = `severity-${anomaly.severity}`;
        return `
            <div class="anomaly ${severityClass}">
                <div class="anomaly-header">
                    <span class="badge badge-${anomaly.severity}">${anomaly.severity.toUpperCase()}</span>
                    <strong>${anomaly.type.replace(/_/g, ' ').toUpperCase()}</strong>
                </div>
                <p>${anomaly.description}</p>
                ${anomaly.affected_table ? `<small>Table: <code>${anomaly.affected_table}</code></small>` : ''}
                ${anomaly.affected_service ? `<small>Service: <code>${anomaly.affected_service}</code></small>` : ''}
            </div>
        `;
    }).join('');
}

// Render bipartite graph with D3.js
function renderGraph() {
    const container = document.getElementById('bipartite-graph');
    const width = container.clientWidth || 1200;
    const height = 800;

    // Clear existing
    container.innerHTML = '';

    // Create SVG
    const svg = d3.select('#bipartite-graph')
        .append('svg')
        .attr('width', width)
        .attr('height', height);

    // Prepare data
    const services = report.services.slice(0, 20); // Limit to top 20
    const tables = report.tables.slice(0, 20); // Limit to top 20
    const edges = report.edges.filter(e =>
        services.some(s => s.ip === e.service) &&
        tables.some(t => t.full_name === e.table)
    );

    // Layout parameters
    const leftX = 150;
    const rightX = width - 150;
    const serviceY = (i) => (height / (services.length + 1)) * (i + 1);
    const tableY = (i) => (height / (tables.length + 1)) * (i + 1);

    // Draw edges
    svg.selectAll('.edge')
        .data(edges)
        .enter()
        .append('path')
        .attr('class', 'edge')
        .attr('d', d => {
            const sIdx = services.findIndex(s => s.ip === d.service);
            const tIdx = tables.findIndex(t => t.full_name === d.table);
            const sy = serviceY(sIdx);
            const ty = tableY(tIdx);
            const midX = (leftX + rightX) / 2;
            return `M ${leftX} ${sy} C ${midX} ${sy}, ${midX} ${ty}, ${rightX} ${ty}`;
        })
        .attr('stroke', '#888')
        .attr('stroke-width', d => Math.min((d.reads + d.writes) / 100, 5))
        .attr('fill', 'none')
        .attr('opacity', 0.4);

    // Draw service nodes
    svg.selectAll('.service-node')
        .data(services)
        .enter()
        .append('circle')
        .attr('class', 'service-node')
        .attr('cx', leftX)
        .attr('cy', (d, i) => serviceY(i))
        .attr('r', 8)
        .attr('fill', '#4a90e2');

    // Draw service labels
    svg.selectAll('.service-label')
        .data(services)
        .enter()
        .append('text')
        .attr('class', 'service-label')
        .attr('x', leftX - 15)
        .attr('y', (d, i) => serviceY(i) + 4)
        .attr('text-anchor', 'end')
        .text(d => d.k8s_service || d.ip);

    // Draw table nodes
    svg.selectAll('.table-node')
        .data(tables)
        .enter()
        .append('rect')
        .attr('class', 'table-node')
        .attr('x', rightX - 6)
        .attr('y', (d, i) => tableY(i) - 6)
        .attr('width', 12)
        .attr('height', 12)
        .attr('fill', '#50c878');

    // Draw table labels
    svg.selectAll('.table-label')
        .data(tables)
        .enter()
        .append('text')
        .attr('class', 'table-label')
        .attr('x', rightX + 15)
        .attr('y', (d, i) => tableY(i) + 4)
        .text(d => d.full_name);
}

// Tab navigation
function showTab(tabName) {
    // Update buttons
    document.querySelectorAll('.tab-button').forEach(btn => {
        btn.classList.remove('active');
    });
    document.getElementById(`tab-${tabName}`).classList.add('active');

    // Update panels
    document.querySelectorAll('.panel').forEach(panel => {
        panel.classList.remove('active');
    });
    document.getElementById(`${tabName}-panel`).classList.add('active');

    currentTab = tabName;

    // Re-render graph when tab is shown (for proper sizing)
    if (tabName === 'graph') {
        renderGraph();
    }
}

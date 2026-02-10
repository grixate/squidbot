<script lang="ts">
  import { onMount } from 'svelte';
  import { UIButton, UISelect, UIScrollArea } from '$lib/components/ui';
  import { fetchJSON, parseError } from '$lib/http';
  import { formatDateTime } from '$lib/utils/time';

  type Health = {
    status?: Record<string, unknown>;
    metrics?: Record<string, number>;
  };

  type Summary = {
    range: string;
    from: string;
    to: string;
    totals: Record<string, number>;
    tokenTotal: number;
    series: Array<{ day: string; token_total: number }>;
  };

  type LogRow = {
    type?: string;
    name?: string;
    status?: string;
    jobId?: string;
    sessionId?: string;
    createdAt?: string;
    summary?: string;
    error?: string;
  };

  const rangeOptions = [
    { value: '7d', label: 'Last 7 Days' },
    { value: '30d', label: 'Last 30 Days' }
  ];

  const logTypeOptions = [
    { value: '', label: 'All Types' },
    { value: 'tool', label: 'Tool' },
    { value: 'cron', label: 'Cron' },
    { value: 'heartbeat', label: 'Heartbeat' }
  ];

  let loading = true;
  let refreshing = false;
  let error = '';

  let selectedRange = '7d';
  let selectedType = '';
  let fromLocal = '';
  let toLocal = '';
  let limit = 120;

  let health: Health = {};
  let summary: Summary | null = null;
  let logs: LogRow[] = [];

  function toRFC3339(value: string): string {
    const trimmed = value.trim();
    if (!trimmed) return '';
    const parsed = new Date(trimmed);
    if (Number.isNaN(parsed.getTime())) return '';
    return parsed.toISOString();
  }

  function renderLogName(row: LogRow): string {
    if (row.type === 'tool') {
      return row.name || 'Tool';
    }
    if (row.type === 'cron') {
      return row.jobId || 'Cron Job';
    }
    if (row.type === 'heartbeat') {
      return row.status || 'Heartbeat';
    }
    return 'Event';
  }

  async function loadLogs() {
    const params = new URLSearchParams();
    if (selectedType) params.set('type', selectedType);
    if (limit > 0) params.set('limit', String(limit));

    const from = toRFC3339(fromLocal);
    const to = toRFC3339(toLocal);
    if (from) params.set('from', from);
    if (to) params.set('to', to);

    const suffix = params.toString() ? `?${params.toString()}` : '';
    const logsResp = await fetchJSON<{ logs: LogRow[] }>(`/api/manage/analytics/logs${suffix}`);
    logs = logsResp.logs || [];
  }

  async function loadData() {
    loading = true;
    error = '';
    try {
      const [healthResp, summaryResp] = await Promise.all([
        fetchJSON<Health>('/api/manage/analytics/health'),
        fetchJSON<Summary>(`/api/manage/analytics/summary?range=${encodeURIComponent(selectedRange)}`)
      ]);
      health = healthResp;
      summary = summaryResp;
      await loadLogs();
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  async function refreshSummaryAndLogs() {
    refreshing = true;
    error = '';
    try {
      summary = await fetchJSON<Summary>(`/api/manage/analytics/summary?range=${encodeURIComponent(selectedRange)}`);
      await loadLogs();
    } catch (err) {
      error = parseError(err);
    } finally {
      refreshing = false;
    }
  }

  async function resetFilters() {
    selectedType = '';
    fromLocal = '';
    toLocal = '';
    limit = 120;
    await refreshSummaryAndLogs();
  }

  onMount(async () => {
    await loadData();
  });
</script>

<section>
  <header class="page-header">
    <p class="kicker">Observability</p>
    <h2>Analytics</h2>
    <p class="muted">System health, trends, and runtime events.</p>
  </header>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
    <section class="panel analytics-controls">
      <div>
        <h3>Range</h3>
        <UISelect
          id="analytics-range"
          name="analytics_range"
          bind:value={selectedRange}
          options={rangeOptions}
          ariaLabel="Analytics range"
          placeholder="Select range…"
        />
      </div>
      <div class="inline">
        <UIButton type="button" onclick={refreshSummaryAndLogs} disabled={refreshing}>Refresh</UIButton>
        <p class="muted">{summary ? `${formatDateTime(summary.from)} to ${formatDateTime(summary.to)}` : 'No range loaded.'}</p>
      </div>
    </section>

    <div class="stat-grid analytics-stats">
      <article class="panel stat-card">
        <h3>Tokens</h3>
        <p class="stat-value">{summary?.tokenTotal ?? 0}</p>
        <p class="muted">Range total</p>
      </article>
      <article class="panel stat-card">
        <h3>Errors</h3>
        <p class="stat-value">{summary?.totals?.errors ?? 0}</p>
        <p class="muted">Logged errors</p>
      </article>
      <article class="panel stat-card">
        <h3>Tool Events</h3>
        <p class="stat-value">{summary?.totals?.tool_events ?? 0}</p>
        <p class="muted">In selected range</p>
      </article>
      <article class="panel stat-card">
        <h3>Cron Runs</h3>
        <p class="stat-value">{summary?.totals?.cron_runs ?? 0}</p>
        <p class="muted">In selected range</p>
      </article>
      <article class="panel stat-card">
        <h3>Heartbeat Runs</h3>
        <p class="stat-value">{summary?.totals?.heartbeat_runs ?? 0}</p>
        <p class="muted">In selected range</p>
      </article>
    </div>

    <section class="panel">
      <h3>Token Trend</h3>
      {#if !summary || !summary.series || summary.series.length === 0}
        <p class="muted">No token data in this range.</p>
      {:else}
        <table>
          <thead>
            <tr>
              <th scope="col">Day</th>
              <th scope="col">Total Tokens</th>
            </tr>
          </thead>
          <tbody>
            {#each summary.series as point}
              <tr>
                <td>{point.day}</td>
                <td>{point.token_total}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </section>

    <section class="panel">
      <h3>Health</h3>
      <div class="kv-grid">
        {#each Object.entries(health.status || {}) as [key, value]}
          <p><strong>{key}</strong>: {String(value)}</p>
        {/each}
      </div>
      <h3>Runtime Metrics</h3>
      <div class="kv-grid">
        {#each Object.entries(health.metrics || {}) as [key, value]}
          <p><strong>{key}</strong>: {String(value)}</p>
        {/each}
      </div>
    </section>

    <section class="panel">
      <h3>Recent Events</h3>
      <div class="analytics-filter-grid">
        <div>
          <label for="analytics-type">Type</label>
          <UISelect
            id="analytics-type"
            name="analytics_type"
            bind:value={selectedType}
            options={logTypeOptions}
            ariaLabel="Event type"
            placeholder="Filter type…"
          />
        </div>
        <div>
          <label for="analytics-from">From</label>
          <input id="analytics-from" name="analytics_from" type="datetime-local" bind:value={fromLocal} />
        </div>
        <div>
          <label for="analytics-to">To</label>
          <input id="analytics-to" name="analytics_to" type="datetime-local" bind:value={toLocal} />
        </div>
        <div>
          <label for="analytics-limit">Limit</label>
          <input id="analytics-limit" name="analytics_limit" type="number" min="1" max="5000" bind:value={limit} />
        </div>
      </div>
      <div class="inline analytics-filter-actions">
        <UIButton type="button" onclick={refreshSummaryAndLogs} disabled={refreshing}>Apply Filters</UIButton>
        <UIButton type="button" className="ui-button-subtle" onclick={resetFilters} disabled={refreshing}>Reset</UIButton>
      </div>

      {#if logs.length === 0}
        <p class="muted">No logs available for this filter.</p>
      {:else}
        <UIScrollArea className="logs-scroll">
          <table>
            <thead>
              <tr>
                <th scope="col">Type</th>
                <th scope="col">Name</th>
                <th scope="col">Created</th>
                <th scope="col">Summary</th>
                <th scope="col">Error</th>
              </tr>
            </thead>
            <tbody>
              {#each logs as row}
                <tr>
                  <td>{String(row.type || '')}</td>
                  <td>{renderLogName(row)}</td>
                  <td>{formatDateTime(row.createdAt || '')}</td>
                  <td>{String(row.summary || '')}</td>
                  <td>{String(row.error || '')}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </UIScrollArea>
      {/if}
    </section>
  {/if}

  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

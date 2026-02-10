<script lang="ts">
  import { onMount } from 'svelte';
  import { fetchJSON, parseError } from '$lib/http';

  let loading = true;
  let error = '';
  let health: { status?: Record<string, unknown>; metrics?: Record<string, number> } = {};
  let logs: Array<Record<string, unknown>> = [];

  async function loadData() {
    loading = true;
    error = '';
    try {
      const [healthResp, logsResp] = await Promise.all([
        fetchJSON<{ status: Record<string, unknown>; metrics: Record<string, number> }>(
          '/api/manage/analytics/health'
        ),
        fetchJSON<{ logs: Array<Record<string, unknown>> }>('/api/manage/analytics/logs?limit=100')
      ]);
      health = healthResp;
      logs = logsResp.logs || [];
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  onMount(async () => {
    await loadData();
  });
</script>

<section>
  <header class="page-header">
    <p class="kicker">Observability</p>
    <h2>Analytics</h2>
    <p class="muted">System health and recent runtime events.</p>
  </header>

  {#if loading}
    <p class="muted">Loading...</p>
  {:else}
    <section class="panel">
      <h3>Health</h3>
      <div class="kv-grid">
        {#each Object.entries(health.status || {}) as [key, value]}
          <p><strong>{key}</strong>: {String(value)}</p>
        {/each}
      </div>
      <h3>Metrics</h3>
      <div class="kv-grid">
        {#each Object.entries(health.metrics || {}) as [key, value]}
          <p><strong>{key}</strong>: {String(value)}</p>
        {/each}
      </div>
    </section>

    <section class="panel">
      <h3>Recent Logs</h3>
      {#if logs.length === 0}
        <p class="muted">No logs available.</p>
      {:else}
        <div class="logs-table-wrap">
          <table>
            <thead>
              <tr>
                <th scope="col">Type</th>
                <th scope="col">Created At</th>
                <th scope="col">Summary</th>
                <th scope="col">Error</th>
              </tr>
            </thead>
            <tbody>
              {#each logs as row}
                <tr>
                  <td>{String(row.type || '')}</td>
                  <td>{String(row.createdAt || '')}</td>
                  <td>{String(row.summary || '')}</td>
                  <td>{String(row.error || '')}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  {/if}

  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

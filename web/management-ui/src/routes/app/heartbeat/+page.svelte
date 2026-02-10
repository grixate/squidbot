<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from 'bits-ui';
  import { fetchJSON, parseError } from '$lib/http';

  type HeartbeatRun = {
    id: string;
    triggered_by: string;
    status: string;
    error?: string;
    preview?: string;
    started_at: string;
    finished_at: string;
    duration_ms: number;
  };

  let loading = true;
  let error = '';
  let intervalSec = 1800;
  let runtimeOnline = false;
  let running = false;
  let nextRunAt = '';
  let lastResult = '';
  let recentRuns: HeartbeatRun[] = [];

  async function loadData() {
    loading = true;
    error = '';
    try {
      const data = await fetchJSON<{
        runtimeOnline: boolean;
        intervalSec: number;
        running: boolean;
        nextRunAt?: string;
        recentRuns: HeartbeatRun[];
      }>('/api/manage/heartbeat');
      runtimeOnline = data.runtimeOnline;
      intervalSec = data.intervalSec || 1800;
      running = !!data.running;
      nextRunAt = data.nextRunAt || '';
      recentRuns = data.recentRuns || [];
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  async function saveInterval() {
    error = '';
    try {
      await fetchJSON('/api/manage/heartbeat/interval', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ intervalSec })
      });
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function triggerNow() {
    error = '';
    try {
      const response = await fetchJSON<{ result: string }>('/api/manage/heartbeat/trigger', {
        method: 'POST'
      });
      lastResult = response.result || '';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  onMount(async () => {
    await loadData();
  });
</script>

<section>
  <header class="page-header">
    <p class="kicker">Runtime</p>
    <h2>Heartbeat</h2>
    <p class="muted">Observe cycles and tune beat rate.</p>
  </header>

  {#if loading}
    <p class="muted">Loading...</p>
  {:else}
    <div class="panel">
      <p><strong>Runtime:</strong> {runtimeOnline ? 'Online' : 'Offline'}</p>
      <p><strong>Service:</strong> {running ? 'Running' : 'Stopped'}</p>
      <p><strong>Next Run:</strong> {nextRunAt || 'n/a'}</p>
      <label for="interval">Beat Interval (seconds)</label>
      <input id="interval" name="heartbeat_interval" bind:value={intervalSec} type="number" min="1" />
      <div class="inline">
        <Button.Root type="button" onclick={saveInterval}>Save Interval</Button.Root>
        <Button.Root type="button" onclick={triggerNow} disabled={!runtimeOnline}>Trigger Now</Button.Root>
      </div>
      {#if lastResult}
        <p class="muted">{lastResult}</p>
      {/if}
    </div>

    <section class="panel">
      <h3>Recent Runs</h3>
      {#if recentRuns.length === 0}
        <p class="muted">No runs recorded yet.</p>
      {:else}
        <ul class="run-list">
          {#each recentRuns as run}
            <li>
              <p><strong>{run.status}</strong> / {run.triggered_by} / {run.started_at}</p>
              {#if run.preview}
                <p>{run.preview}</p>
              {/if}
              {#if run.error}
                <p class="error">{run.error}</p>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}

  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

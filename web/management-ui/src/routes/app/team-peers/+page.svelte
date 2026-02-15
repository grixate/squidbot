<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from 'bits-ui';
  import { fetchJSON, parseError } from '$lib/http';

  type FederationSettings = {
    enabled: boolean;
    nodeId: string;
    listenAddr: string;
    requestTimeoutSec: number;
    maxRetries: number;
    retryBackoffMs: number;
    autoFallback: boolean;
    allowFromNodeIDs: string[];
  };

  type Peer = {
    id: string;
    baseUrl: string;
    authTokenSet: boolean;
    enabled: boolean;
    capabilities: string[];
    roles: string[];
    priority: number;
    maxConcurrent: number;
    maxQueue: number;
    healthEndpoint?: string;
  };

  type PeerHealth = {
    peer_id: string;
    available: boolean;
    queue_depth: number;
    max_queue: number;
    active_runs: number;
    error?: string;
    updated_at?: string;
    response_time_ms?: number;
  };

  type DeliveryAttempt = {
    peer_id: string;
    attempt: number;
    status_code?: number;
    retryable: boolean;
    error?: string;
    started_at: string;
    finished_at: string;
    duration_ms: number;
    idempotency_key?: string;
  };

  type DelegationRun = {
    id: string;
    origin_node_id?: string;
    session_id?: string;
    task: string;
    label?: string;
    status: string;
    error?: string;
    created_at: string;
    started_at?: string;
    finished_at?: string;
    peer_id?: string;
    fallback_chain?: string[];
    context?: { mode?: string };
    delivery_attempts?: DeliveryAttempt[];
    result?: {
      summary?: string;
      output?: string;
      artifact_paths?: string[];
    };
  };

  type PeerForm = {
    id: string;
    baseUrl: string;
    authToken: string;
    enabled: boolean;
    capabilities: string;
    roles: string;
    priority: number;
    maxConcurrent: number;
    maxQueue: number;
    healthEndpoint: string;
  };

  const emptySettings = (): FederationSettings => ({
    enabled: false,
    nodeId: '',
    listenAddr: '127.0.0.1:18900',
    requestTimeoutSec: 30,
    maxRetries: 2,
    retryBackoffMs: 500,
    autoFallback: true,
    allowFromNodeIDs: []
  });

  const emptyPeerForm = (): PeerForm => ({
    id: '',
    baseUrl: '',
    authToken: '',
    enabled: true,
    capabilities: '',
    roles: '',
    priority: 100,
    maxConcurrent: 2,
    maxQueue: 64,
    healthEndpoint: '/api/federation/health'
  });

  let loading = true;
  let savingSettings = false;
  let savingPeer = false;
  let error = '';
  let notice = '';
  let settings = emptySettings();
  let allowFromText = '';
  let peers: Peer[] = [];
  let healthByPeer: Record<string, PeerHealth> = {};
  let runs: DelegationRun[] = [];
  let selectedRunID = '';
  let selectedRun: DelegationRun | null = null;
  let editingPeerID = '';
  let peerForm = emptyPeerForm();

  const parseCSV = (value: string): string[] =>
    value
      .split(',')
      .map((item) => item.trim())
      .filter((item) => item.length > 0);

  const formatTime = (value?: string): string => (value ? new Date(value).toLocaleString() : 'n/a');

  const peerHealth = (peerID: string): PeerHealth | undefined => healthByPeer[peerID];

  const peerStatusLabel = (peerID: string): string => {
    const health = peerHealth(peerID);
    if (!health) return 'unknown';
    return health.available ? 'healthy' : 'unhealthy';
  };

  async function loadRun(runID: string): Promise<void> {
    if (!runID) return;
    selectedRun = await fetchJSON<DelegationRun>(`/api/manage/federation/runs/${runID}`);
    selectedRunID = runID;
  }

  async function loadData(): Promise<void> {
    loading = true;
    error = '';
    try {
      const [nextSettings, peersResp, runsResp] = await Promise.all([
        fetchJSON<FederationSettings>('/api/manage/federation/settings'),
        fetchJSON<{ peers: Peer[]; health: Record<string, PeerHealth> }>('/api/manage/federation/peers'),
        fetchJSON<{ runs: DelegationRun[] }>('/api/manage/federation/runs?limit=40')
      ]);
      settings = nextSettings || emptySettings();
      allowFromText = (settings.allowFromNodeIDs || []).join(', ');
      peers = peersResp.peers || [];
      healthByPeer = peersResp.health || {};
      runs = runsResp.runs || [];
      if (selectedRunID) {
        await loadRun(selectedRunID);
      }
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  async function saveSettings(): Promise<void> {
    savingSettings = true;
    error = '';
    notice = '';
    try {
      await fetchJSON('/api/manage/federation/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: settings.enabled,
          nodeId: settings.nodeId,
          listenAddr: settings.listenAddr,
          requestTimeoutSec: settings.requestTimeoutSec,
          maxRetries: settings.maxRetries,
          retryBackoffMs: settings.retryBackoffMs,
          autoFallback: settings.autoFallback,
          allowFromNodeIDs: parseCSV(allowFromText)
        })
      });
      notice = 'Federation settings saved. Restart runtime to apply changes.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    } finally {
      savingSettings = false;
    }
  }

  function startCreatePeer(): void {
    editingPeerID = '';
    peerForm = emptyPeerForm();
  }

  function startEditPeer(peer: Peer): void {
    editingPeerID = peer.id;
    peerForm = {
      id: peer.id,
      baseUrl: peer.baseUrl,
      authToken: '',
      enabled: peer.enabled,
      capabilities: (peer.capabilities || []).join(', '),
      roles: (peer.roles || []).join(', '),
      priority: peer.priority,
      maxConcurrent: peer.maxConcurrent,
      maxQueue: peer.maxQueue,
      healthEndpoint: peer.healthEndpoint || '/api/federation/health'
    };
  }

  async function savePeer(): Promise<void> {
    if (!peerForm.id.trim()) {
      error = 'Peer id is required.';
      return;
    }
    if (!peerForm.baseUrl.trim()) {
      error = 'Peer base URL is required.';
      return;
    }
    savingPeer = true;
    error = '';
    notice = '';
    try {
      if (editingPeerID) {
        const patchBody: Record<string, unknown> = {
          baseUrl: peerForm.baseUrl.trim(),
          enabled: peerForm.enabled,
          capabilities: parseCSV(peerForm.capabilities),
          roles: parseCSV(peerForm.roles),
          priority: peerForm.priority,
          maxConcurrent: peerForm.maxConcurrent,
          maxQueue: peerForm.maxQueue,
          healthEndpoint: peerForm.healthEndpoint.trim()
        };
        if (peerForm.authToken.trim() !== '') {
          patchBody.authToken = peerForm.authToken.trim();
        }
        await fetchJSON(`/api/manage/federation/peers/${encodeURIComponent(editingPeerID)}`, {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(patchBody)
        });
      } else {
        await fetchJSON('/api/manage/federation/peers', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id: peerForm.id.trim(),
            baseUrl: peerForm.baseUrl.trim(),
            authToken: peerForm.authToken.trim(),
            enabled: peerForm.enabled,
            capabilities: parseCSV(peerForm.capabilities),
            roles: parseCSV(peerForm.roles),
            priority: peerForm.priority,
            maxConcurrent: peerForm.maxConcurrent,
            maxQueue: peerForm.maxQueue,
            healthEndpoint: peerForm.healthEndpoint.trim()
          })
        });
      }
      notice = 'Peer saved. Restart runtime to apply changes.';
      startCreatePeer();
      await loadData();
    } catch (err) {
      error = parseError(err);
    } finally {
      savingPeer = false;
    }
  }

  async function testPeer(peerID: string): Promise<void> {
    error = '';
    notice = '';
    try {
      const payload = await fetchJSON<{ ok: boolean; responseMs?: number; error?: string }>(
        `/api/manage/federation/peers/${encodeURIComponent(peerID)}/test`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ originNodeId: settings.nodeId || 'mission-control' })
        }
      );
      if (payload.ok) {
        notice = `Peer ${peerID} healthy (${payload.responseMs ?? 0} ms).`;
      } else {
        error = payload.error || `Peer ${peerID} test failed.`;
      }
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function togglePeer(peer: Peer): Promise<void> {
    error = '';
    notice = '';
    try {
      await fetchJSON(`/api/manage/federation/peers/${encodeURIComponent(peer.id)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !peer.enabled })
      });
      notice = `Peer ${peer.id} ${peer.enabled ? 'disabled' : 'enabled'}.`;
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function deletePeer(peerID: string): Promise<void> {
    error = '';
    notice = '';
    try {
      await fetchJSON(`/api/manage/federation/peers/${encodeURIComponent(peerID)}`, {
        method: 'DELETE'
      });
      notice = `Peer ${peerID} removed.`;
      if (editingPeerID === peerID) {
        startCreatePeer();
      }
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
    <p class="kicker">Federation</p>
    <h2>Team / Peers</h2>
    <p class="muted">Manage peer mesh routing, connectivity, and delegated runs.</p>
  </header>

  {#if loading}
    <p class="muted">Loading...</p>
  {:else}
    <div class="split-grid">
      <section class="panel">
        <h3>Node Settings</h3>
        <label class="checkbox">
          <input type="checkbox" bind:checked={settings.enabled} />
          <span>Enable federation</span>
        </label>
        <label class="checkbox">
          <input type="checkbox" bind:checked={settings.autoFallback} />
          <span>Automatic fallback</span>
        </label>
        <label for="node-id">Node ID</label>
        <input id="node-id" name="node_id" bind:value={settings.nodeId} type="text" />
        <label for="listen-addr">Listen Address</label>
        <input id="listen-addr" name="listen_addr" bind:value={settings.listenAddr} type="text" />
        <label for="allow-nodes">Allowlist Node IDs (comma separated)</label>
        <input id="allow-nodes" name="allow_nodes" bind:value={allowFromText} type="text" />
        <label for="request-timeout">Request Timeout (sec)</label>
        <input id="request-timeout" name="request_timeout" bind:value={settings.requestTimeoutSec} type="number" min="1" />
        <label for="max-retries">Max Retries</label>
        <input id="max-retries" name="max_retries" bind:value={settings.maxRetries} type="number" min="0" />
        <label for="retry-backoff">Retry Backoff (ms)</label>
        <input id="retry-backoff" name="retry_backoff" bind:value={settings.retryBackoffMs} type="number" min="0" />
        <Button.Root type="button" onclick={saveSettings} disabled={savingSettings}>Save Settings</Button.Root>
      </section>

      <section class="panel">
        <h3>{editingPeerID ? `Edit Peer: ${editingPeerID}` : 'Add Peer'}</h3>
        <label for="peer-id">Peer ID</label>
        <input id="peer-id" name="peer_id" bind:value={peerForm.id} type="text" disabled={editingPeerID !== ''} />
        <label for="peer-url">Base URL</label>
        <input id="peer-url" name="peer_url" bind:value={peerForm.baseUrl} type="text" placeholder="http://host:port" />
        <label for="peer-token">
          Auth Token {editingPeerID ? '(set only to rotate/replace)' : ''}
        </label>
        <input id="peer-token" name="peer_token" bind:value={peerForm.authToken} type="password" />
        <label for="peer-capabilities">Capabilities (comma separated)</label>
        <input id="peer-capabilities" name="peer_capabilities" bind:value={peerForm.capabilities} type="text" />
        <label for="peer-roles">Roles (comma separated)</label>
        <input id="peer-roles" name="peer_roles" bind:value={peerForm.roles} type="text" />
        <label for="peer-priority">Priority (lower first)</label>
        <input id="peer-priority" name="peer_priority" bind:value={peerForm.priority} type="number" />
        <label for="peer-max-concurrent">Max Concurrent</label>
        <input id="peer-max-concurrent" name="peer_max_concurrent" bind:value={peerForm.maxConcurrent} type="number" min="1" />
        <label for="peer-max-queue">Max Queue</label>
        <input id="peer-max-queue" name="peer_max_queue" bind:value={peerForm.maxQueue} type="number" min="1" />
        <label for="peer-health-endpoint">Health Endpoint</label>
        <input id="peer-health-endpoint" name="peer_health_endpoint" bind:value={peerForm.healthEndpoint} type="text" />
        <label class="checkbox">
          <input type="checkbox" bind:checked={peerForm.enabled} />
          <span>Peer enabled</span>
        </label>
        <div class="inline">
          <Button.Root type="button" onclick={savePeer} disabled={savingPeer}>
            {editingPeerID ? 'Save Peer' : 'Add Peer'}
          </Button.Root>
          <Button.Root type="button" onclick={startCreatePeer}>Reset</Button.Root>
        </div>
      </section>
    </div>

    <section class="panel">
      <h3>Configured Peers</h3>
      {#if peers.length === 0}
        <p class="muted">No peers configured.</p>
      {:else}
        <div class="peer-grid">
          {#each peers as peer}
            <article class="peer-card">
              <div class="peer-head">
                <h4>{peer.id}</h4>
                <p class={peerStatusLabel(peer.id) === 'healthy' ? 'success' : 'error'}>{peerStatusLabel(peer.id)}</p>
              </div>
              <p class="muted">{peer.baseUrl}</p>
              <p><strong>Auth:</strong> {peer.authTokenSet ? 'configured' : 'not set'}</p>
              <p><strong>Capabilities:</strong> {(peer.capabilities || []).join(', ') || 'none'}</p>
              <p><strong>Roles:</strong> {(peer.roles || []).join(', ') || 'none'}</p>
              <p>
                <strong>Priority/Limits:</strong>
                {peer.priority} / {peer.maxConcurrent} concurrent / {peer.maxQueue} queue
              </p>
              {#if peerHealth(peer.id)}
                <p class="muted">
                  Queue {peerHealth(peer.id)?.queue_depth ?? 0}/{peerHealth(peer.id)?.max_queue ?? peer.maxQueue} |
                  Active {peerHealth(peer.id)?.active_runs ?? 0} |
                  RTT {peerHealth(peer.id)?.response_time_ms ?? 0} ms
                </p>
                <p class="muted">Updated: {formatTime(peerHealth(peer.id)?.updated_at)}</p>
                {#if peerHealth(peer.id)?.error}
                  <p class="error">{peerHealth(peer.id)?.error}</p>
                {/if}
              {/if}
              <div class="inline">
                <Button.Root type="button" onclick={() => startEditPeer(peer)}>Edit</Button.Root>
                <Button.Root type="button" onclick={() => togglePeer(peer)}>
                  {peer.enabled ? 'Disable' : 'Enable'}
                </Button.Root>
                <Button.Root type="button" onclick={() => testPeer(peer.id)}>Test</Button.Root>
                <Button.Root type="button" onclick={() => deletePeer(peer.id)}>Delete</Button.Root>
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </section>

    <section class="split-grid">
      <section class="panel">
        <h3>Recent Delegated Runs</h3>
        {#if runs.length === 0}
          <p class="muted">No federated runs recorded.</p>
        {:else}
          <ul class="run-list">
            {#each runs as run}
              <li>
                <button type="button" class="run-link" onclick={() => loadRun(run.id)}>
                  <strong>{run.status}</strong> / {run.id} / {run.peer_id || 'unassigned'}
                  <br />
                  <span class="muted">{run.task}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="panel">
        <h3>Run Detail</h3>
        {#if !selectedRun}
          <p class="muted">Select a run to inspect routing and result details.</p>
        {:else}
          <div class="kv-grid">
            <p><strong>ID</strong></p>
            <p>{selectedRun.id}</p>
            <p><strong>Status</strong></p>
            <p>{selectedRun.status}</p>
            <p><strong>Origin</strong></p>
            <p>{selectedRun.origin_node_id || 'n/a'}</p>
            <p><strong>Selected Peer</strong></p>
            <p>{selectedRun.peer_id || 'n/a'}</p>
            <p><strong>Context Mode</strong></p>
            <p>{selectedRun.context?.mode || 'n/a'}</p>
            <p><strong>Created</strong></p>
            <p>{formatTime(selectedRun.created_at)}</p>
            <p><strong>Started</strong></p>
            <p>{formatTime(selectedRun.started_at)}</p>
            <p><strong>Finished</strong></p>
            <p>{formatTime(selectedRun.finished_at)}</p>
          </div>
          <p><strong>Fallback Chain:</strong> {(selectedRun.fallback_chain || []).join(' -> ') || 'none'}</p>
          {#if selectedRun.error}
            <p class="error">{selectedRun.error}</p>
          {/if}
          <h4>Status Timeline</h4>
          <ul class="run-list">
            {#if selectedRun.delivery_attempts && selectedRun.delivery_attempts.length > 0}
              {#each selectedRun.delivery_attempts as attempt}
                <li>
                  <strong>{attempt.peer_id}</strong> attempt {attempt.attempt} |
                  status {attempt.status_code || 0} |
                  {attempt.retryable ? 'retryable' : 'terminal'} |
                  {attempt.duration_ms} ms
                  {#if attempt.error}
                    <p class="error">{attempt.error}</p>
                  {/if}
                </li>
              {/each}
            {:else}
              <li class="muted">No delivery attempts recorded.</li>
            {/if}
          </ul>
          {#if selectedRun.result}
            <h4>Result</h4>
            <p>{selectedRun.result.summary || 'No summary'}</p>
            {#if selectedRun.result.output}
              <pre class="run-output">{selectedRun.result.output}</pre>
            {/if}
            <p><strong>Artifacts:</strong> {(selectedRun.result.artifact_paths || []).join(', ') || 'none'}</p>
          {/if}
        {/if}
      </section>
    </section>
  {/if}

  {#if notice}
    <p class="success" aria-live="polite">{notice}</p>
  {/if}
  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

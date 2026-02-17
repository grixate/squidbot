<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from 'bits-ui';
  import { fetchJSON, parseError } from '$lib/http';

  type ProviderItem = {
    id: string;
    label: string;
    apiBase?: string;
    model?: string;
    hasApiKey: boolean;
  };

  type ChannelItem = {
    id: string;
    label: string;
    kind: string;
    enabled: boolean;
    tokenSet: boolean;
    allowFrom?: string[];
    endpoint?: string;
  };

  type Settings = {
    providers: { active: string; items: ProviderItem[] };
    channels: { telegram: { enabled: boolean; tokenSet: boolean; allowFrom: string[] }; items?: Record<string, ChannelItem> };
    runtime: {
      heartbeatIntervalSec: number;
      mailboxSize: number;
      routing?: {
        channelModel?: string;
        branchModel?: string;
        workerModel?: string;
        compactorModel?: string;
        cortexModel?: string;
        taskOverrides?: Record<string, string>;
        fallbacks?: Record<string, string[]>;
        rateLimitCooldownSec?: number;
      };
      compaction?: {
        enabled: boolean;
        backgroundThresholdPct: number;
        aggressiveThresholdPct: number;
        emergencyThresholdPct: number;
      };
      cortex?: {
        enabled: boolean;
        bulletinIntervalSec: number;
        bulletinMaxWords: number;
      };
    };
    management: { host: string; port: number; publicBaseUrl: string; serveInGateway: boolean };
  };

  let loading = true;
  let error = '';
  let success = '';

  let settings: Settings | null = null;

  let provider = '';
  let providerApiKey = '';
  let providerApiBase = '';
  let providerModel = '';
  let providerTest = '';

  let channelItems: ChannelItem[] = [];
  let selectedChannelID = 'telegram';
  let channelEnabled = false;
  let channelToken = '';
  let channelAllow = '';
  let channelEndpoint = '';
  let channelAuthToken = '';

  let heartbeatIntervalSec = 1800;
  let mailboxSize = 64;
  let routingChannelModel = '';
  let routingBranchModel = '';
  let routingWorkerModel = '';
  let routingCompactorModel = '';
  let routingCortexModel = '';
  let routingTaskOverrides = '{}';
  let routingFallbacks = '{}';
  let routingRateLimitCooldownSec = 20;
  let compactionEnabled = true;
  let compactionBackgroundThresholdPct = 72;
  let compactionAggressiveThresholdPct = 84;
  let compactionEmergencyThresholdPct = 94;
  let cortexEnabled = true;
  let cortexBulletinIntervalSec = 120;
  let cortexBulletinMaxWords = 180;

  let currentPassword = '';
  let newPassword = '';

  function resetProviderFields() {
    const item = settings?.providers.items.find((entry) => entry.id === provider);
    providerApiKey = '';
    providerApiBase = item?.apiBase || '';
    providerModel = item?.model || '';
    providerTest = '';
  }

  async function loadData() {
    loading = true;
    error = '';
    success = '';
    try {
      settings = await fetchJSON<Settings>('/api/manage/settings');
      provider = settings.providers.active || settings.providers.items[0]?.id || '';
      resetProviderFields();
      channelItems = Object.values(settings.channels.items || {});
      if (channelItems.length === 0) {
        channelItems = [
          {
            id: 'telegram',
            label: 'Telegram',
            kind: 'core',
            enabled: settings.channels.telegram.enabled,
            tokenSet: settings.channels.telegram.tokenSet,
            allowFrom: settings.channels.telegram.allowFrom || []
          }
        ];
      }
      selectedChannelID = channelItems.find((entry) => entry.id === 'telegram')?.id || channelItems[0]?.id || 'telegram';
      hydrateChannelSelection();
      heartbeatIntervalSec = settings.runtime.heartbeatIntervalSec || 1800;
      mailboxSize = settings.runtime.mailboxSize || 64;
      routingChannelModel = settings.runtime.routing?.channelModel || '';
      routingBranchModel = settings.runtime.routing?.branchModel || '';
      routingWorkerModel = settings.runtime.routing?.workerModel || '';
      routingCompactorModel = settings.runtime.routing?.compactorModel || '';
      routingCortexModel = settings.runtime.routing?.cortexModel || '';
      routingTaskOverrides = JSON.stringify(settings.runtime.routing?.taskOverrides || {}, null, 2);
      routingFallbacks = JSON.stringify(settings.runtime.routing?.fallbacks || {}, null, 2);
      routingRateLimitCooldownSec = settings.runtime.routing?.rateLimitCooldownSec || 20;
      compactionEnabled = settings.runtime.compaction?.enabled ?? true;
      compactionBackgroundThresholdPct = settings.runtime.compaction?.backgroundThresholdPct ?? 72;
      compactionAggressiveThresholdPct = settings.runtime.compaction?.aggressiveThresholdPct ?? 84;
      compactionEmergencyThresholdPct = settings.runtime.compaction?.emergencyThresholdPct ?? 94;
      cortexEnabled = settings.runtime.cortex?.enabled ?? true;
      cortexBulletinIntervalSec = settings.runtime.cortex?.bulletinIntervalSec ?? 120;
      cortexBulletinMaxWords = settings.runtime.cortex?.bulletinMaxWords ?? 180;
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  async function testProvider() {
    error = '';
    providerTest = 'Testing...';
    try {
      const response = await fetchJSON<{ ok: boolean; error?: string }>('/api/manage/settings/provider/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider,
          apiKey: providerApiKey,
          apiBase: providerApiBase,
          model: providerModel
        })
      });
      if (!response.ok) {
        providerTest = 'Failed';
        error = response.error || 'Provider test failed';
        return;
      }
      providerTest = 'OK';
    } catch (err) {
      providerTest = 'Failed';
      error = parseError(err);
    }
  }

  async function saveProvider() {
    error = '';
    success = '';
    try {
      await fetchJSON('/api/manage/settings/provider', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider,
          apiKey: providerApiKey,
          apiBase: providerApiBase,
          model: providerModel,
          activate: true
        })
      });
      success = 'Provider settings saved.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  function hydrateChannelSelection() {
    const selected = channelItems.find((entry) => entry.id === selectedChannelID);
    if (!selected) return;
    channelEnabled = !!selected.enabled;
    channelToken = '';
    channelAllow = (selected.allowFrom || []).join(',');
    channelEndpoint = selected.endpoint || '';
    channelAuthToken = '';
  }

  async function saveChannel() {
    error = '';
    success = '';
    try {
      await fetchJSON(`/api/manage/settings/channels/${selectedChannelID}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: channelEnabled,
          token: channelToken,
          allowFrom: channelAllow
            .split(',')
            .map((value) => value.trim())
            .filter((value) => value.length > 0),
          endpoint: channelEndpoint,
          authToken: channelAuthToken
        })
      });
      success = `Channel settings saved for ${selectedChannelID}.`;
      channelToken = '';
      channelAuthToken = '';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveRuntime() {
    error = '';
    success = '';
    try {
      await fetchJSON('/api/manage/settings/runtime', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ heartbeatIntervalSec, mailboxSize })
      });
      success = 'Runtime settings saved.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveRouting() {
    error = '';
    success = '';
    try {
      const taskOverrides = JSON.parse(routingTaskOverrides || '{}');
      const fallbacks = JSON.parse(routingFallbacks || '{}');
      await fetchJSON('/api/manage/settings/routing', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          channelModel: routingChannelModel,
          branchModel: routingBranchModel,
          workerModel: routingWorkerModel,
          compactorModel: routingCompactorModel,
          cortexModel: routingCortexModel,
          taskOverrides,
          fallbacks,
          rateLimitCooldownSec: routingRateLimitCooldownSec
        })
      });
      success = 'Routing settings saved.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveCompaction() {
    error = '';
    success = '';
    try {
      await fetchJSON('/api/manage/settings/compaction', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: compactionEnabled,
          backgroundThresholdPct: compactionBackgroundThresholdPct,
          aggressiveThresholdPct: compactionAggressiveThresholdPct,
          emergencyThresholdPct: compactionEmergencyThresholdPct
        })
      });
      success = 'Compaction settings saved.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveCortex() {
    error = '';
    success = '';
    try {
      await fetchJSON('/api/manage/settings/cortex', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: cortexEnabled,
          bulletinIntervalSec: cortexBulletinIntervalSec,
          bulletinMaxWords: cortexBulletinMaxWords
        })
      });
      success = 'Cortex settings saved.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function savePassword() {
    error = '';
    success = '';
    try {
      await fetchJSON('/api/manage/settings/password', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ currentPassword, newPassword })
      });
      success = 'Password updated.';
      currentPassword = '';
      newPassword = '';
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
    <p class="kicker">Configuration</p>
    <h2>Settings</h2>
    <p class="muted">Manage providers, channels, runtime, and security.</p>
  </header>

  {#if loading}
    <p class="muted">Loading...</p>
  {:else}
    <div class="split-grid">
      <section class="panel">
        <h3>Provider</h3>
        <label for="provider-id">Provider</label>
        <select
          id="provider-id"
          name="provider_id"
          bind:value={provider}
          onchange={() => {
            resetProviderFields();
          }}
        >
          {#each settings?.providers.items || [] as item}
            <option value={item.id}>{item.label}</option>
          {/each}
        </select>
        <label for="provider-api-key">API Key</label>
        <input id="provider-api-key" name="provider_api_key" bind:value={providerApiKey} type="password" />
        <label for="provider-api-base">API Base</label>
        <input id="provider-api-base" name="provider_api_base" bind:value={providerApiBase} type="text" />
        <label for="provider-model">Model</label>
        <input id="provider-model" name="provider_model" bind:value={providerModel} type="text" />
        <div class="inline">
          <Button.Root type="button" onclick={testProvider}>Test Provider</Button.Root>
          <Button.Root type="button" onclick={saveProvider}>Save Provider</Button.Root>
        </div>
        {#if providerTest}
          <p class="muted">{providerTest}</p>
        {/if}
      </section>

      <section class="panel">
        <h3>Channel</h3>
        <label for="channel-id">Channel</label>
        <select
          id="channel-id"
          name="channel_id"
          bind:value={selectedChannelID}
          onchange={() => {
            hydrateChannelSelection();
          }}
        >
          {#each channelItems as item}
            <option value={item.id}>{item.label} ({item.kind})</option>
          {/each}
        </select>
        <label class="checkbox" for="channel-enabled">
          <input id="channel-enabled" name="channel_enabled" bind:checked={channelEnabled} type="checkbox" />
          Enable Channel
        </label>
        <label for="channel-token">Token</label>
        <input id="channel-token" name="channel_token" bind:value={channelToken} type="password" />
        <label for="channel-allow">Allow List (comma-separated)</label>
        <input id="channel-allow" name="channel_allow" bind:value={channelAllow} type="text" />
        <label for="channel-endpoint">Endpoint</label>
        <input id="channel-endpoint" name="channel_endpoint" bind:value={channelEndpoint} type="text" />
        <label for="channel-auth">Auth Token</label>
        <input id="channel-auth" name="channel_auth" bind:value={channelAuthToken} type="password" />
        <Button.Root type="button" onclick={saveChannel}>Save Channel</Button.Root>
      </section>
    </div>

    <div class="split-grid">
      <section class="panel">
        <h3>Runtime</h3>
        <label for="heartbeat-interval">Heartbeat Interval (seconds)</label>
        <input
          id="heartbeat-interval"
          name="heartbeat_interval_sec"
          bind:value={heartbeatIntervalSec}
          type="number"
          min="1"
        />
        <label for="mailbox-size">Mailbox Size</label>
        <input id="mailbox-size" name="mailbox_size" bind:value={mailboxSize} type="number" min="1" />
        <Button.Root type="button" onclick={saveRuntime}>Save Runtime</Button.Root>
      </section>

      <section class="panel">
        <h3>Security</h3>
        <label for="current-password">Current Password</label>
        <input id="current-password" name="current_password" bind:value={currentPassword} type="password" />
        <label for="new-password">New Password</label>
        <input id="new-password" name="new_password" bind:value={newPassword} type="password" />
        <Button.Root type="button" onclick={savePassword}>Update Password</Button.Root>
      </section>
    </div>

    <div class="split-grid">
      <section class="panel">
        <h3>Routing</h3>
        <label for="routing-channel-model">Channel Model</label>
        <input id="routing-channel-model" bind:value={routingChannelModel} type="text" />
        <label for="routing-branch-model">Branch Model</label>
        <input id="routing-branch-model" bind:value={routingBranchModel} type="text" />
        <label for="routing-worker-model">Worker Model</label>
        <input id="routing-worker-model" bind:value={routingWorkerModel} type="text" />
        <label for="routing-compactor-model">Compactor Model</label>
        <input id="routing-compactor-model" bind:value={routingCompactorModel} type="text" />
        <label for="routing-cortex-model">Cortex Model</label>
        <input id="routing-cortex-model" bind:value={routingCortexModel} type="text" />
        <label for="routing-task-overrides">Task Overrides (JSON)</label>
        <textarea id="routing-task-overrides" rows={5} bind:value={routingTaskOverrides}></textarea>
        <label for="routing-fallbacks">Fallbacks (JSON)</label>
        <textarea id="routing-fallbacks" rows={5} bind:value={routingFallbacks}></textarea>
        <label for="routing-cooldown">Rate Limit Cooldown (sec)</label>
        <input id="routing-cooldown" type="number" min="0" bind:value={routingRateLimitCooldownSec} />
        <Button.Root type="button" onclick={saveRouting}>Save Routing</Button.Root>
      </section>

      <section class="panel">
        <h3>Compaction</h3>
        <label class="checkbox" for="compaction-enabled">
          <input id="compaction-enabled" type="checkbox" bind:checked={compactionEnabled} />
          Enable Compaction
        </label>
        <label for="compaction-bg">Background Threshold (%)</label>
        <input id="compaction-bg" type="number" min="1" max="100" bind:value={compactionBackgroundThresholdPct} />
        <label for="compaction-aggr">Aggressive Threshold (%)</label>
        <input id="compaction-aggr" type="number" min="1" max="100" bind:value={compactionAggressiveThresholdPct} />
        <label for="compaction-emergency">Emergency Threshold (%)</label>
        <input
          id="compaction-emergency"
          type="number"
          min="1"
          max="100"
          bind:value={compactionEmergencyThresholdPct}
        />
        <Button.Root type="button" onclick={saveCompaction}>Save Compaction</Button.Root>
      </section>
    </div>

    <section class="panel">
      <h3>Cortex</h3>
      <label class="checkbox" for="cortex-enabled">
        <input id="cortex-enabled" type="checkbox" bind:checked={cortexEnabled} />
        Enable Cortex
      </label>
      <label for="cortex-interval">Bulletin Interval (sec)</label>
      <input id="cortex-interval" type="number" min="1" bind:value={cortexBulletinIntervalSec} />
      <label for="cortex-max-words">Bulletin Max Words</label>
      <input id="cortex-max-words" type="number" min="10" bind:value={cortexBulletinMaxWords} />
      <Button.Root type="button" onclick={saveCortex}>Save Cortex</Button.Root>
    </section>
  {/if}

  {#if success}
    <p class="success" aria-live="polite">{success}</p>
  {/if}
  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

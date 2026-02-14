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
    runtime: { heartbeatIntervalSec: number; mailboxSize: number };
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
  {/if}

  {#if success}
    <p class="success" aria-live="polite">{success}</p>
  {/if}
  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

<script lang="ts">
  import { onMount } from 'svelte';
  import { Tabs } from 'bits-ui';
  import { UIButton, UISelect, UISwitch } from '$lib/components/ui';
  import { fetchJSON, parseError } from '$lib/http';

  type ProviderItem = {
    id: string;
    label: string;
    apiBase?: string;
    model?: string;
    hasApiKey: boolean;
  };

  type GenericScaffold = {
    label?: string;
    kind?: string;
    enabled: boolean;
    endpoint?: string;
    headers?: Record<string, string>;
    metadata?: Record<string, string>;
  };

  type Settings = {
    providers: { active: string; items: ProviderItem[] };
    channels: {
      telegram: { enabled: boolean; tokenSet: boolean; allowFrom: string[] };
      scaffolds?: Record<string, GenericScaffold>;
    };
    runtime: { heartbeatIntervalSec: number; mailboxSize: number };
    management: { host: string; port: number; publicBaseUrl: string; serveInGateway: boolean };
  };

  let loading = true;
  let error = '';
  let success = '';
  let activeTab = 'providers';

  let settings: Settings | null = null;

  let provider = '';
  let providerApiKey = '';
  let providerApiBase = '';
  let providerModel = '';
  let providerTest = '';
  let providerOptions: Array<{ value: string; label: string }> = [];

  let telegramEnabled = false;
  let telegramToken = '';
  let telegramAllow = '';

  let scaffoldID = '';
  let scaffoldOptions: Array<{ value: string; label: string }> = [];
  let scaffoldLabel = '';
  let scaffoldKind = '';
  let scaffoldEnabled = false;
  let scaffoldEndpoint = '';
  let scaffoldAuthToken = '';

  let heartbeatIntervalSec = 1800;
  let mailboxSize = 64;

  let currentPassword = '';
  let newPassword = '';

  let snapshotImportText = '';

  function clearStatus() {
    error = '';
    success = '';
  }

  function resetProviderFields() {
    const item = settings?.providers.items.find((entry) => entry.id === provider);
    providerApiKey = '';
    providerApiBase = item?.apiBase || '';
    providerModel = item?.model || '';
    providerTest = '';
  }

  function resetScaffoldFields() {
    const scaffold = settings?.channels.scaffolds?.[scaffoldID];
    scaffoldLabel = scaffold?.label || scaffoldID;
    scaffoldKind = scaffold?.kind || 'generic';
    scaffoldEnabled = !!scaffold?.enabled;
    scaffoldEndpoint = scaffold?.endpoint || '';
    scaffoldAuthToken = '';
  }

  async function loadData() {
    loading = true;
    clearStatus();
    try {
      settings = await fetchJSON<Settings>('/api/manage/settings');

      provider = settings.providers.active || settings.providers.items[0]?.id || '';
      providerOptions = (settings.providers.items || []).map((item) => ({ value: item.id, label: item.label }));
      resetProviderFields();

      telegramEnabled = settings.channels.telegram.enabled;
      telegramAllow = (settings.channels.telegram.allowFrom || []).join(',');

      const scaffoldIDs = Object.keys(settings.channels.scaffolds || {});
      scaffoldOptions = scaffoldIDs.map((id) => ({ value: id, label: id }));
      if (!scaffoldID) {
        scaffoldID = scaffoldIDs[0] || 'generic';
      }
      if (!scaffoldOptions.find((item) => item.value === scaffoldID)) {
        scaffoldOptions = [...scaffoldOptions, { value: scaffoldID, label: scaffoldID }];
      }
      resetScaffoldFields();

      heartbeatIntervalSec = settings.runtime.heartbeatIntervalSec || 1800;
      mailboxSize = settings.runtime.mailboxSize || 64;
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  async function testProvider() {
    clearStatus();
    providerTest = 'Testing…';
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
    clearStatus();
    try {
      await fetchJSON(`/api/manage/settings/providers/${encodeURIComponent(provider)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
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

  async function activateProvider() {
    clearStatus();
    try {
      await fetchJSON('/api/manage/settings/providers/active', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider })
      });
      success = 'Provider activated.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function removeProvider() {
    clearStatus();
    try {
      await fetchJSON(`/api/manage/settings/providers/${encodeURIComponent(provider)}`, {
        method: 'DELETE'
      });
      success = 'Provider removed.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveTelegram() {
    clearStatus();
    try {
      await fetchJSON('/api/manage/settings/channels/telegram', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          enabled: telegramEnabled,
          token: telegramToken,
          allowFrom: telegramAllow
            .split(',')
            .map((value) => value.trim())
            .filter((value) => value.length > 0)
        })
      });
      success = 'Telegram settings saved.';
      telegramToken = '';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveScaffold() {
    clearStatus();
    try {
      await fetchJSON(`/api/manage/settings/channels/${encodeURIComponent(scaffoldID)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          label: scaffoldLabel,
          kind: scaffoldKind,
          enabled: scaffoldEnabled,
          endpoint: scaffoldEndpoint,
          authToken: scaffoldAuthToken,
          headers: {},
          metadata: {}
        })
      });
      success = 'Scaffold channel saved.';
      scaffoldAuthToken = '';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function saveRuntime() {
    clearStatus();
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
    clearStatus();
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

  async function exportSnapshot() {
    clearStatus();
    try {
      const snapshot = await fetchJSON<Record<string, unknown>>('/api/manage/snapshot/export');
      const blob = new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json' });
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.download = `mission-control-snapshot-${new Date().toISOString().slice(0, 10)}.json`;
      anchor.click();
      URL.revokeObjectURL(href);
      success = 'Snapshot exported.';
    } catch (err) {
      error = parseError(err);
    }
  }

  async function importSnapshot() {
    clearStatus();
    if (!snapshotImportText.trim()) {
      error = 'Snapshot JSON is required.';
      return;
    }
    try {
      const payload = JSON.parse(snapshotImportText);
      await fetchJSON('/api/manage/snapshot/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      success = 'Snapshot imported.';
      await loadData();
    } catch (err) {
      error = parseError(err);
    }
  }

  async function onSnapshotFileChange(event: Event) {
    const target = event.target as HTMLInputElement;
    const file = target.files?.[0];
    if (!file) return;
    snapshotImportText = await file.text();
  }

  $: if (provider && settings) {
    const exists = settings.providers.items.some((entry) => entry.id === provider);
    if (!exists) {
      provider = settings.providers.items[0]?.id || '';
    }
  }

  $: if (scaffoldID) {
    resetScaffoldFields();
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
    <p class="muted">Loading…</p>
  {:else}
    <Tabs.Root bind:value={activeTab}>
      <Tabs.List class="tabs-list panel">
        <Tabs.Trigger value="providers" class="tab-trigger">Providers</Tabs.Trigger>
        <Tabs.Trigger value="channels" class="tab-trigger">Channels</Tabs.Trigger>
        <Tabs.Trigger value="runtime" class="tab-trigger">Runtime</Tabs.Trigger>
        <Tabs.Trigger value="security" class="tab-trigger">Security</Tabs.Trigger>
      </Tabs.List>

      <Tabs.Content value="providers" class="panel">
        <h3>Providers</h3>
        <label for="provider-id">Provider</label>
        <UISelect
          id="provider-id"
          name="provider_id"
          bind:value={provider}
          options={providerOptions}
          ariaLabel="Provider"
          placeholder="Select provider…"
        />
        <label for="provider-api-key">API Key</label>
        <input id="provider-api-key" name="provider_api_key" bind:value={providerApiKey} type="password" autocomplete="off" />
        <label for="provider-api-base">API Base</label>
        <input id="provider-api-base" name="provider_api_base" bind:value={providerApiBase} type="text" autocomplete="off" />
        <label for="provider-model">Model</label>
        <input id="provider-model" name="provider_model" bind:value={providerModel} type="text" autocomplete="off" />
        <div class="inline">
          <UIButton type="button" onclick={testProvider}>Test Provider</UIButton>
          <UIButton type="button" onclick={saveProvider}>Save + Activate</UIButton>
          <UIButton type="button" className="ui-button-subtle" onclick={activateProvider}>Activate Existing</UIButton>
          <UIButton type="button" className="ui-button-danger" onclick={removeProvider}>Remove</UIButton>
        </div>
        {#if providerTest}
          <p class="muted">{providerTest}</p>
        {/if}
      </Tabs.Content>

      <Tabs.Content value="channels" class="panel">
        <h3>Channels</h3>
        <h4>Telegram</h4>
        <div class="checkbox">
          <UISwitch bind:checked={telegramEnabled} ariaLabel="Enable Telegram" />
          <span>Enable Telegram</span>
        </div>
        <label for="telegram-token">Token</label>
        <input id="telegram-token" name="telegram_token" bind:value={telegramToken} type="password" autocomplete="off" />
        <label for="telegram-allow">Allow List (comma-separated)</label>
        <input id="telegram-allow" name="telegram_allow" bind:value={telegramAllow} type="text" autocomplete="off" />
        <UIButton type="button" onclick={saveTelegram}>Save Telegram</UIButton>

        <hr />

        <h4>Scaffold Channels</h4>
        <label for="scaffold-id">Channel ID</label>
        <UISelect
          id="scaffold-id"
          name="scaffold_id"
          bind:value={scaffoldID}
          options={scaffoldOptions}
          ariaLabel="Scaffold channel"
          placeholder="Select scaffold…"
        />
        <label for="scaffold-label">Label</label>
        <input id="scaffold-label" name="scaffold_label" bind:value={scaffoldLabel} type="text" autocomplete="off" />
        <label for="scaffold-kind">Kind</label>
        <input id="scaffold-kind" name="scaffold_kind" bind:value={scaffoldKind} type="text" autocomplete="off" />
        <div class="checkbox">
          <UISwitch bind:checked={scaffoldEnabled} ariaLabel="Enable scaffold channel" />
          <span>Enable Scaffold Channel</span>
        </div>
        <label for="scaffold-endpoint">Endpoint</label>
        <input id="scaffold-endpoint" name="scaffold_endpoint" bind:value={scaffoldEndpoint} type="url" autocomplete="off" />
        <label for="scaffold-auth-token">Auth Token (optional)</label>
        <input
          id="scaffold-auth-token"
          name="scaffold_auth_token"
          bind:value={scaffoldAuthToken}
          type="password"
          autocomplete="off"
        />
        <UIButton type="button" onclick={saveScaffold}>Save Scaffold</UIButton>
      </Tabs.Content>

      <Tabs.Content value="runtime" class="panel">
        <h3>Runtime</h3>
        <label for="heartbeat-interval">Heartbeat Interval (seconds)</label>
        <input
          id="heartbeat-interval"
          name="heartbeat_interval_sec"
          bind:value={heartbeatIntervalSec}
          type="number"
          min="1"
          autocomplete="off"
        />
        <label for="mailbox-size">Mailbox Size</label>
        <input id="mailbox-size" name="mailbox_size" bind:value={mailboxSize} type="number" min="1" autocomplete="off" />
        <UIButton type="button" onclick={saveRuntime}>Save Runtime</UIButton>

        <hr />

        <h3>Snapshots</h3>
        <div class="inline">
          <UIButton type="button" onclick={exportSnapshot}>Export Snapshot</UIButton>
          <input name="snapshot_file" type="file" accept="application/json" onchange={onSnapshotFileChange} />
        </div>
        <label for="snapshot-import">Import JSON</label>
        <textarea id="snapshot-import" name="snapshot_import" bind:value={snapshotImportText} rows={8}></textarea>
        <UIButton type="button" onclick={importSnapshot}>Import Snapshot</UIButton>
      </Tabs.Content>

      <Tabs.Content value="security" class="panel">
        <h3>Security</h3>
        <label for="current-password">Current Password</label>
        <input id="current-password" name="current_password" bind:value={currentPassword} type="password" autocomplete="current-password" />
        <label for="new-password">New Password</label>
        <input id="new-password" name="new_password" bind:value={newPassword} type="password" autocomplete="new-password" />
        <UIButton type="button" onclick={savePassword}>Update Password</UIButton>
      </Tabs.Content>
    </Tabs.Root>
  {/if}

  {#if success}
    <p class="success" aria-live="polite">{success}</p>
  {/if}
  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</section>

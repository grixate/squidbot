<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { Button } from 'bits-ui';
  import { fetchJSON, parseError } from '$lib/http';

  type Stage = 'loading' | 'onboarding' | 'login';

  type ProviderInfo = {
    id: string;
    label: string;
    requiresApiKey: boolean;
    requiresModel: boolean;
    defaultApiBase?: string;
    defaultModel?: string;
  };

  type ChannelInfo = { id: string; label: string; kind?: string };
  type ChannelState = { enabled?: boolean; allowFrom?: string[]; endpoint?: string };

  let stage: Stage = 'loading';
  let stateNote = 'Loading setup state...';
  let error = '';

  let setupToken = '';
  let providers: ProviderInfo[] = [];
  let provider = '';
  let apiKey = '';
  let apiBase = '';
  let model = '';
  let password = '';
  let providerTestResult = '';
  let channels: ChannelInfo[] = [];
  let channelStates: Record<string, { enabled: boolean; token: string; allow: string; endpoint: string; authToken: string }> = {};

  let loginPassword = '';

  const clearError = () => (error = '');
  const setError = (value: unknown) => {
    error = parseError(value);
  };

  function updateProviderDefaults() {
    const selected = providers.find((entry) => entry.id === provider);
    if (!selected) return;
    if (!apiBase && selected.defaultApiBase) {
      apiBase = selected.defaultApiBase;
    }
    if (!model && selected.defaultModel) {
      model = selected.defaultModel;
    }
  }

  async function loadState() {
    clearError();
    stage = 'loading';
    const setupState = await fetchJSON<{
      setupComplete: boolean;
      providers: ProviderInfo[];
      channels?: ChannelInfo[];
      current?: {
        provider?: { id?: string; apiBase?: string; model?: string };
        channels?: Record<string, ChannelState>;
      };
    }>('/api/setup/state');

    providers = setupState.providers || [];
    provider = setupState.current?.provider?.id || providers[0]?.id || '';
    apiBase = setupState.current?.provider?.apiBase || '';
    model = setupState.current?.provider?.model || '';
    channels = setupState.channels || [];
    channelStates = {};
    for (const channel of channels) {
      const current = setupState.current?.channels?.[channel.id] || {};
      channelStates[channel.id] = {
        enabled: !!current.enabled,
        token: '',
        allow: (current.allowFrom || []).join(','),
        endpoint: current.endpoint || '',
        authToken: ''
      };
    }
    if (!channelStates.telegram) {
      channelStates.telegram = { enabled: false, token: '', allow: '', endpoint: '', authToken: '' };
    }
    updateProviderDefaults();

    if (!setupState.setupComplete) {
      stage = 'onboarding';
      stateNote = 'Complete onboarding to unlock Mission Control.';
      return;
    }

    const session = await fetchJSON<{ authenticated: boolean }>('/api/auth/session');
    if (session.authenticated) {
      await goto('/app/mission-control');
      return;
    }

    stage = 'login';
    stateNote = 'Setup complete. Sign in to continue.';
  }

  async function runProviderTest() {
    clearError();
    providerTestResult = 'Testing...';
    try {
      const result = await fetchJSON<{ ok: boolean; error?: string }>('/api/setup/provider/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ setupToken, provider, apiKey, apiBase, model })
      });
      if (result.ok) {
        providerTestResult = 'OK';
        return;
      }
      providerTestResult = 'Failed';
      error = result.error || 'Provider test failed';
    } catch (err) {
      providerTestResult = 'Failed';
      setError(err);
    }
  }

  async function completeSetup() {
    clearError();
    try {
      await fetchJSON('/api/setup/complete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          setupToken,
          provider,
          apiKey,
          apiBase,
          model,
          channels: Object.entries(channelStates).map(([id, channel]) => ({
            id,
            enabled: channel.enabled,
            token: channel.token,
            allowFrom: channel.allow
              .split(',')
              .map((value) => value.trim())
              .filter((value) => value.length > 0),
            endpoint: channel.endpoint,
            authToken: channel.authToken
          })),
          channel: {
            id: 'telegram',
            enabled: channelStates.telegram?.enabled || false,
            token: channelStates.telegram?.token || '',
            allowFrom: (channelStates.telegram?.allow || '')
              .split(',')
              .map((value) => value.trim())
              .filter((value) => value.length > 0)
          },
          password
        })
      });
      const url = new URL(window.location.href);
      url.searchParams.delete('setup_token');
      window.history.replaceState({}, '', url.toString());
      setupToken = '';
      password = '';
      await loadState();
    } catch (err) {
      setError(err);
    }
  }

  async function login() {
    clearError();
    try {
      await fetchJSON('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: loginPassword })
      });
      loginPassword = '';
      await goto('/app/mission-control');
    } catch (err) {
      setError(err);
    }
  }

  onMount(async () => {
    setupToken = new URLSearchParams(window.location.search).get('setup_token') || '';
    try {
      await loadState();
    } catch (err) {
      setError(err);
      stage = 'loading';
    }
  });
</script>

<main class="onboarding-shell">
  <header class="page-header">
    <p class="kicker">Squidbot</p>
    <h1>Mission Control Setup</h1>
    <p class="muted">{stateNote}</p>
  </header>

  {#if stage === 'loading'}
    <section class="panel">
      <p class="muted">Loading...</p>
    </section>
  {/if}

  {#if stage === 'onboarding'}
    <section class="panel">
      <h2>Onboarding</h2>
      <label for="provider">Provider</label>
      <select id="provider" name="provider" bind:value={provider} onchange={updateProviderDefaults}>
        {#each providers as item}
          <option value={item.id}>{item.label}</option>
        {/each}
      </select>

      <label for="api-key">API Key</label>
      <input id="api-key" name="api_key" bind:value={apiKey} type="password" autocomplete="off" />

      <label for="api-base">API Base</label>
      <input id="api-base" name="api_base" bind:value={apiBase} type="text" autocomplete="off" />

      <label for="model">Model</label>
      <input id="model" name="model" bind:value={model} type="text" autocomplete="off" />

      <div class="inline">
        <Button.Root type="button" onclick={runProviderTest}>Test Connection</Button.Root>
        <span class="result">{providerTestResult}</span>
      </div>

      <hr />
      {#each channels as channel}
        <h3>{channel.label}</h3>
        <label class="checkbox" for={`channel-enabled-${channel.id}`}>
          <input
            id={`channel-enabled-${channel.id}`}
            name={`channel_enabled_${channel.id}`}
            bind:checked={channelStates[channel.id].enabled}
            type="checkbox"
          />
          Enable {channel.label}
        </label>
        <label for={`channel-token-${channel.id}`}>Token</label>
        <input
          id={`channel-token-${channel.id}`}
          name={`channel_token_${channel.id}`}
          bind:value={channelStates[channel.id].token}
          type="password"
          autocomplete="off"
        />
        <label for={`channel-allow-${channel.id}`}>Allow List</label>
        <input
          id={`channel-allow-${channel.id}`}
          name={`channel_allow_${channel.id}`}
          bind:value={channelStates[channel.id].allow}
          type="text"
          autocomplete="off"
        />
        <label for={`channel-endpoint-${channel.id}`}>Endpoint</label>
        <input
          id={`channel-endpoint-${channel.id}`}
          name={`channel_endpoint_${channel.id}`}
          bind:value={channelStates[channel.id].endpoint}
          type="text"
          autocomplete="off"
        />
        <label for={`channel-auth-${channel.id}`}>Auth Token</label>
        <input
          id={`channel-auth-${channel.id}`}
          name={`channel_auth_${channel.id}`}
          bind:value={channelStates[channel.id].authToken}
          type="password"
          autocomplete="off"
        />
      {/each}

      <hr />
      <label for="password">Management Password (min 12 chars)</label>
      <input id="password" name="password" bind:value={password} type="password" autocomplete="new-password" />
      <Button.Root type="button" onclick={completeSetup}>Complete Setup</Button.Root>
    </section>
  {/if}

  {#if stage === 'login'}
    <section class="panel">
      <h2>Sign In</h2>
      <label for="login-password">Password</label>
      <input
        id="login-password"
        name="login_password"
        bind:value={loginPassword}
        type="password"
        autocomplete="current-password"
      />
      <Button.Root type="button" onclick={login}>Sign In</Button.Root>
    </section>
  {/if}

  {#if error}
    <p class="error" aria-live="polite">{error}</p>
  {/if}
</main>

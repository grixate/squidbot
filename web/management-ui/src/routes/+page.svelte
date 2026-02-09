<script lang="ts">
  import { onMount } from 'svelte';
  import { Button } from 'bits-ui';

  type Stage = 'loading' | 'onboarding' | 'login' | 'manage';

  type ProviderInfo = {
    id: string;
    label: string;
    requiresApiKey: boolean;
    requiresModel: boolean;
    defaultApiBase?: string;
    defaultModel?: string;
  };

  let stage: Stage = 'loading';
  let stateNote = 'Loading setup state...';
  let error = '';

  let setupToken = '';
  let providers: ProviderInfo[] = [];
  let provider = '';
  let apiKey = '';
  let apiBase = '';
  let model = '';

  let telegramEnabled = false;
  let telegramToken = '';
  let telegramAllow = '';

  let password = '';
  let providerTestResult = '';

  let loginPassword = '';
  let manageMessage = 'Loading management state...';

  const clearError = () => {
    error = '';
  };

  const setError = (message: unknown) => {
    if (typeof message === 'string') {
      error = message;
      return;
    }
    error = 'Request failed';
  };

  async function fetchJSON<T>(url: string, options?: RequestInit): Promise<T> {
    const response = await fetch(url, options);
    if (!response.ok) {
      const body = await response.text();
      throw new Error(body || `Request failed (${response.status})`);
    }
    return (await response.json()) as T;
  }

  function updateProviderDefaults() {
    const selected = providers.find((item) => item.id === provider);
    if (!selected) {
      return;
    }
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
      current?: {
        provider?: { id?: string; apiBase?: string; model?: string };
        channels?: { telegram?: { enabled?: boolean; allowFrom?: string[] } };
      };
    }>('/api/setup/state');

    providers = setupState.providers || [];
    provider = setupState.current?.provider?.id || providers[0]?.id || '';
    apiBase = setupState.current?.provider?.apiBase || '';
    model = setupState.current?.provider?.model || '';
    telegramEnabled = !!setupState.current?.channels?.telegram?.enabled;
    telegramAllow = (setupState.current?.channels?.telegram?.allowFrom || []).join(',');
    updateProviderDefaults();

    if (!setupState.setupComplete) {
      stage = 'onboarding';
      stateNote = 'Complete first-time onboarding to enable management login.';
      return;
    }

    const session = await fetchJSON<{ authenticated: boolean }>('/api/auth/session');
    if (session.authenticated) {
      stage = 'manage';
      stateNote = 'Authenticated management session.';
      await refreshManagePlaceholder();
      return;
    }

    stage = 'login';
    stateNote = 'Setup is complete. Sign in to access management.';
  }

  async function runProviderTest() {
    clearError();
    providerTestResult = 'Testing...';
    try {
      const result = await fetchJSON<{ ok: boolean; error?: string }>('/api/setup/provider/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          setupToken,
          provider,
          apiKey,
          apiBase,
          model
        })
      });
      if (result.ok) {
        providerTestResult = 'Connection OK';
        return;
      }
      providerTestResult = 'Failed';
      setError(result.error || 'Provider test failed');
    } catch (err) {
      providerTestResult = 'Failed';
      setError((err as Error).message);
    }
  }

  async function completeSetup() {
    clearError();
    try {
      await fetchJSON<{ ok: boolean }>('/api/setup/complete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          setupToken,
          provider,
          apiKey,
          apiBase,
          model,
          channel: {
            id: 'telegram',
            enabled: telegramEnabled,
            token: telegramToken,
            allowFrom: telegramAllow
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
      setError((err as Error).message);
    }
  }

  async function login() {
    clearError();
    try {
      await fetchJSON<{ ok: boolean }>('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: loginPassword })
      });
      loginPassword = '';
      await loadState();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  async function refreshManagePlaceholder() {
    clearError();
    try {
      const response = await fetchJSON<{ message: string }>('/api/manage/placeholder');
      manageMessage = response.message || 'Management ready.';
    } catch (err) {
      manageMessage = '';
      setError((err as Error).message);
    }
  }

  async function logout() {
    clearError();
    try {
      await fetchJSON<{ ok: boolean }>('/api/auth/logout', { method: 'POST' });
      await loadState();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  onMount(async () => {
    setupToken = new URLSearchParams(window.location.search).get('setup_token') || '';
    try {
      await loadState();
    } catch (err) {
      setError((err as Error).message);
      stage = 'loading';
    }
  });
</script>

<main>
  <header>
    <p class="kicker">Squidbot Control Plane</p>
    <h1>Onboarding and Management</h1>
    <p class="muted">{stateNote}</p>
  </header>

  {#if stage === 'onboarding'}
    <section class="panel">
      <h2>Initial setup</h2>
      <p class="muted">
        Provider tests run only when you click Test connection. Remote providers may incur small usage cost.
      </p>

      <label for="provider">Provider</label>
      <select id="provider" bind:value={provider} onchange={updateProviderDefaults}>
        {#each providers as item}
          <option value={item.id}>{item.label}</option>
        {/each}
      </select>

      <label for="api-key">API key</label>
      <input id="api-key" bind:value={apiKey} type="password" autocomplete="off" />

      <label for="api-base">API base</label>
      <input id="api-base" bind:value={apiBase} type="text" autocomplete="off" />

      <label for="model">Model</label>
      <input id="model" bind:value={model} type="text" autocomplete="off" />

      <div class="inline">
        <Button.Root type="button" onclick={runProviderTest}>Test connection</Button.Root>
        <span class="result">{providerTestResult}</span>
      </div>

      <hr />

      <label class="checkbox" for="telegram-enabled">
        <input id="telegram-enabled" bind:checked={telegramEnabled} type="checkbox" />
        Enable Telegram channel
      </label>

      <label for="telegram-token">Telegram token</label>
      <input id="telegram-token" bind:value={telegramToken} type="password" autocomplete="off" />

      <label for="telegram-allow">Telegram allow list (comma-separated)</label>
      <input id="telegram-allow" bind:value={telegramAllow} type="text" autocomplete="off" />

      <hr />

      <label for="password">Management password (min 12 chars)</label>
      <input id="password" bind:value={password} type="password" autocomplete="new-password" />

      <Button.Root type="button" onclick={completeSetup}>Complete setup</Button.Root>
    </section>
  {/if}

  {#if stage === 'login'}
    <section class="panel">
      <h2>Management login</h2>
      <label for="login-password">Password</label>
      <input id="login-password" bind:value={loginPassword} type="password" autocomplete="current-password" />
      <Button.Root type="button" onclick={login}>Sign in</Button.Root>
    </section>
  {/if}

  {#if stage === 'manage'}
    <section class="panel">
      <h2>Management interface</h2>
      <p class="muted">{manageMessage}</p>
      <div class="inline">
        <Button.Root type="button" onclick={refreshManagePlaceholder}>Refresh</Button.Root>
        <Button.Root type="button" onclick={logout}>Logout</Button.Root>
      </div>
    </section>
  {/if}

  {#if error}
    <p class="error">{error}</p>
  {/if}
</main>

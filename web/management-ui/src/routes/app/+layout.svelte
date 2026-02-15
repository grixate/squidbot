<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { Button } from 'bits-ui';
  import { fetchJSON, parseError } from '$lib/http';

  let loading = true;
  let error = '';

  const navItems = [
    { href: '/app/mission-control', label: 'Mission Control', icon: 'MC' },
    { href: '/app/team-peers', label: 'Team/Peers', icon: 'TP' },
    { href: '/app/heartbeat', label: 'Heartbeat', icon: 'HB' },
    { href: '/app/memory', label: 'Memory', icon: 'MM' },
    { href: '/app/analytics', label: 'Analytics', icon: 'AN' },
    { href: '/app/settings', label: 'Settings', icon: 'ST' }
  ];

  async function checkSession() {
    try {
      const session = await fetchJSON<{ authenticated: boolean; setupComplete: boolean }>('/api/auth/session');
      if (!session.setupComplete) {
        await goto('/');
        return;
      }
      if (!session.authenticated) {
        await goto('/');
        return;
      }
      error = '';
    } catch (err) {
      error = parseError(err);
    } finally {
      loading = false;
    }
  }

  async function logout() {
    try {
      await fetchJSON<{ ok: boolean }>('/api/auth/logout', { method: 'POST' });
      await goto('/');
    } catch (err) {
      error = parseError(err);
    }
  }

  onMount(async () => {
    await checkSession();
  });
</script>

{#if loading}
  <main class="loading-shell">
    <p class="kicker">Mission Control</p>
    <p class="muted">Loading session...</p>
  </main>
{:else}
  <a class="skip-link" href="#main-content">Skip to Main Content</a>
  <div class="shell">
    <aside class="sidebar">
      <div class="sidebar-header">
        <p class="kicker">Squidbot</p>
        <h1>Mission Control</h1>
      </div>
      <nav aria-label="Primary">
        {#each navItems as item}
          <a
            href={item.href}
            class:active={$page.url.pathname === item.href}
            aria-current={$page.url.pathname === item.href ? 'page' : undefined}
          >
            <span aria-hidden="true">{item.icon}</span>
            <span>{item.label}</span>
          </a>
        {/each}
      </nav>
      <Button.Root type="button" class="logout" onclick={logout}>Logout</Button.Root>
      {#if error}
        <p class="error" aria-live="polite">{error}</p>
      {/if}
    </aside>
    <main id="main-content" class="content">
      <slot />
    </main>
  </div>
{/if}

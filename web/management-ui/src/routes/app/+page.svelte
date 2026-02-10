<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';

  onMount(async () => {
    await goto('/app/mission-control');
  });
</script>

<p class="muted">Redirecting…</p>

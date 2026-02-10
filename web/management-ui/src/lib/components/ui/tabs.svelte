<script lang="ts">
  import { Tabs } from 'bits-ui';

  export let value = '';
</script>

<Tabs.Root bind:value class="ui-tabs-root">
  <slot />
</Tabs.Root>

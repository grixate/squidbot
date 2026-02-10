<script lang="ts">
  import { Switch } from 'bits-ui';

  export let checked = false;
  export let disabled = false;
  export let ariaLabel = 'Toggle';
</script>

<Switch.Root bind:checked {disabled} aria-label={ariaLabel} class="ui-switch-root">
  <Switch.Thumb class="ui-switch-thumb" />
</Switch.Root>

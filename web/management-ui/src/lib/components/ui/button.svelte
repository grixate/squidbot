<script lang="ts">
  import { Button } from 'bits-ui';

  export let type: 'button' | 'submit' | 'reset' = 'button';
  export let disabled = false;
  export let className = '';
  export let onclick: ((event: MouseEvent) => void | Promise<void>) | undefined = undefined;
  export let ariaLabel = '';
</script>

<Button.Root {type} {disabled} class={`ui-button ${className}`.trim()} onclick={onclick} aria-label={ariaLabel || undefined}>
  <slot />
</Button.Root>

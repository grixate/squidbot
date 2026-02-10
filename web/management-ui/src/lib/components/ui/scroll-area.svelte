<script lang="ts">
  import { ScrollArea } from 'bits-ui';

  export let className = '';
</script>

<ScrollArea.Root class={`ui-scroll-area ${className}`.trim()}>
  <ScrollArea.Viewport class="ui-scroll-viewport">
    <ScrollArea.Content>
      <slot />
    </ScrollArea.Content>
  </ScrollArea.Viewport>
  <ScrollArea.Scrollbar orientation="vertical" class="ui-scrollbar">
    <ScrollArea.Thumb class="ui-scroll-thumb" />
  </ScrollArea.Scrollbar>
  <ScrollArea.Corner />
</ScrollArea.Root>

<script lang="ts">
  import { AlertDialog } from 'bits-ui';

  export let title = 'Confirm Action';
  export let description = 'Are you sure?';
  export let actionLabel = 'Confirm';
  export let cancelLabel = 'Cancel';
  export let triggerLabel = 'Open';
  export let triggerClassName = '';
  export let onConfirm: () => void | Promise<void> = async () => {};
</script>

<AlertDialog.Root>
  <AlertDialog.Trigger class={`ui-button ${triggerClassName}`.trim()}>{triggerLabel}</AlertDialog.Trigger>
  <AlertDialog.Portal>
    <AlertDialog.Overlay class="ui-dialog-overlay" />
    <AlertDialog.Content class="ui-dialog-content">
      <AlertDialog.Title>{title}</AlertDialog.Title>
      <AlertDialog.Description class="muted">{description}</AlertDialog.Description>
      <div class="inline">
        <AlertDialog.Cancel class="ui-button ui-button-subtle">{cancelLabel}</AlertDialog.Cancel>
        <AlertDialog.Action class="ui-button" onclick={onConfirm}>{actionLabel}</AlertDialog.Action>
      </div>
    </AlertDialog.Content>
  </AlertDialog.Portal>
</AlertDialog.Root>

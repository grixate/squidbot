<script lang="ts">
  import { Select } from 'bits-ui';

  type SelectOption = {
    value: string;
    label: string;
    disabled?: boolean;
  };

  export let value = '';
  export let options: SelectOption[] = [];
  export let id = '';
  export let name = '';
  export let placeholder = 'Select…';
  export let ariaLabel = 'Select';
  export let disabled = false;

  $: selected = options.find((entry) => entry.value === value);

  function handleSelectedChange(next: unknown) {
    const candidate = next as { value?: string } | undefined;
    value = candidate?.value || '';
  }
</script>

<Select.Root items={options} selected={selected} onSelectedChange={handleSelectedChange}>
  <Select.Trigger id={id} aria-label={ariaLabel} disabled={disabled} class="ui-select-trigger">
    <Select.Value {placeholder} />
  </Select.Trigger>
  <Select.Content class="ui-select-content" sideOffset={8}>
    {#each options as option}
      <Select.Item value={option.value} label={option.label} disabled={option.disabled} class="ui-select-item">
        {option.label}
      </Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
{#if name}
  <input type="hidden" {name} {value} />
{/if}

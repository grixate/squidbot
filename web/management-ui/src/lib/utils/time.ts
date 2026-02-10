const formatter = new Intl.DateTimeFormat(undefined, {
  year: 'numeric',
  month: 'short',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit'
});

export function formatDateTime(value: string | number | Date | null | undefined): string {
  if (!value) return 'n/a';
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return 'n/a';
  }
  return formatter.format(date);
}

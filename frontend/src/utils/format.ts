
export function formatDate(value: string): string {
  return value ? new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '-';
}
export function nextStatus(current: string, statuses: readonly string[]): string | null {
  const index = statuses.indexOf(current);
  return index >= 0 && index < statuses.length - 1 ? statuses[index + 1] : null;
}
export function statusTone(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (/approved|accepted|released|completed|signed|closed|pass|ready|online|cleared|succeeded/.test(status)) return 'success';
  if (/failed|fail|rejected|critical|scrap|discard|revoked|urgent|tampered/.test(status)) return 'danger';
  if (/hold|warning|review|pending|restricted|limited|quarantine|incomplete/.test(status)) return 'warning';
  return 'neutral';
}

const JUDGMENT_LABELS: Record<string, string> = {
  pass: '通过', fail: '超限', incomplete: '读数不全', tampered: '复核后改动',
};
export function judgmentLabel(result?: string): string {
  return (result && JUDGMENT_LABELS[result]) || '未判定';
}

const POSITION_LABELS: Record<string, string> = { operator: '操作侧', middle: '中间', drive: '传动侧' };
export function positionLabel(position: string): string {
  return POSITION_LABELS[position] || position || '-';
}

export function formatReading(value?: number | null): string {
  return value === null || value === undefined ? '未录' : value.toFixed(2);
}

import type { DomainRecord, JudgmentRecord, ProofPosition } from '../types/domain';

export const PROOF_POSITIONS: { key: ProofPosition; label: string }[] = [
  { key: 'operation', label: '操作侧' },
  { key: 'center', label: '中间' },
  { key: 'drive', label: '传动侧' },
];

export const DEFAULT_TOLERANCE = 3.0;

export function positionLabel(position: string): string {
  return PROOF_POSITIONS.find((item) => item.key === position)?.label ?? position;
}

export function positionReading(record: DomainRecord | JudgmentRecord, position: ProofPosition): number | null {
  if (position === 'operation') return record.operationSide ?? null;
  if (position === 'center') return record.center ?? null;
  return record.driveSide ?? null;
}

export function missingPositions(record: DomainRecord): ProofPosition[] {
  return PROOF_POSITIONS.map((item) => item.key).filter(
    (position) => positionReading(record, position) === null,
  );
}
export function hasAnyReading(record: DomainRecord): boolean {
  return PROOF_POSITIONS.some((position) => positionReading(record, position.key) !== null);
}

export function activeJudgment(record: DomainRecord): JudgmentRecord | undefined {
  return record.judgments?.find((item) => item.active) ?? record.judgments?.[0];
}

export function formatReading(value: number | null | undefined): string {
  return value === null || value === undefined ? '缺录' : value.toFixed(2);
}

// worstReading returns the largest of the three readings and its position.
export function worstReading(record: DomainRecord): { value: number | null; position: ProofPosition | '' } {
  let value: number | null = null;
  let position: ProofPosition | '' = '';
  for (const candidate of PROOF_POSITIONS) {
    const reading = positionReading(record, candidate.key);
    if (reading === null) continue;
    if (value === null || reading > value) {
      value = reading;
      position = candidate.key;
    }
  }
  return { value, position };
}

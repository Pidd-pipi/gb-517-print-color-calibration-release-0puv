
import type { DomainRecord } from '../../types/domain';
import { StatusBadge } from './StatusBadge';
import { PROOF_POSITIONS, activeJudgment, formatReading, hasAnyReading, missingPositions, positionLabel, positionReading, worstReading } from '../../utils/proof';

interface ColorTableProps {
  records: DomainRecord[];
  title?: string;
  /** toleranceFor resolves the batch ΔE limit for a proof row (by relatedCode). */
  toleranceFor?: (record: DomainRecord) => number | undefined;
  /** proofForRun resolves the linked proof for a batch row (by batch code). */
  proofForRun?: (runCode?: string) => DomainRecord | undefined;
}

function toleranceOf(record: DomainRecord, toleranceFor?: (record: DomainRecord) => number | undefined): number | undefined {
  if (typeof record.colorTolerance === 'number' && record.colorTolerance > 0) return record.colorTolerance;
  return toleranceFor?.(record) ?? activeJudgment(record)?.tolerance;
}

export function ColorTable({ records, title = '色彩读数与证据', toleranceFor, proofForRun }: ColorTableProps) {
  if (!records.length) return null;
  return (
    <section className="color-panel" aria-label={title}>
      <header>
        <div><span className="eyebrow">COLOR EVIDENCE</span><h2>{title}</h2></div>
        <small>操作侧 / 中间 / 传动侧三位置 ΔE，按最差位置判定</small>
      </header>
      <div className="color-table">
        <table>
          <thead>
            <tr>
              <th>样本</th>
              {PROOF_POSITIONS.map((position) => <th key={position.key}>{position.label}</th>)}
              <th>最差位置</th>
              <th>状态</th>
              <th>关联批次</th>
              <th>证据</th>
            </tr>
          </thead>
          <tbody>
            {records.slice(0, 5).map((source) => {
              // On the batch page the row is a run; display its linked proof if any.
              const linkedProof = !hasAnyReading(source) && proofForRun ? proofForRun(source.code) : undefined;
              const item = linkedProof ?? source;
              const proofLike = hasAnyReading(item);
              const tolerance = linkedProof
                ? (source.colorTolerance ?? activeJudgment(item)?.tolerance)
                : toleranceOf(item, toleranceFor);
              const worst = worstReading(item);
              const missing = proofLike ? missingPositions(item) : [];
              const overLimit = proofLike && tolerance !== undefined && worst.value !== null && worst.value > tolerance;
              return (
                <tr key={`${source.id}-${item.id}`} className={overLimit ? 'reading-row reading-row--over' : missing.length ? 'reading-row reading-row--missing' : ''}>
                  <td><strong>{linkedProof ? item.code : source.code}</strong><small>{linkedProof ? item.name : source.name}</small></td>
                  {PROOF_POSITIONS.map((position) => {
                    const reading = positionReading(item, position.key);
                    const isWorst = worst.position === position.key;
                    const cellOver = isWorst && overLimit;
                    return (
                      <td key={position.key} className={cellOver ? 'reading-cell reading-cell--over' : isWorst && reading !== null ? 'reading-cell reading-cell--worst' : reading === null && proofLike ? 'reading-cell reading-cell--missing' : ''}>
                        {proofLike ? (
                          <><span className="reading-value">{formatReading(reading)}</span>{isWorst && reading !== null && <em className="worst-tag">最差</em>}</>
                        ) : `${source.metricValue} ${source.metricUnit}`}
                      </td>
                    );
                  })}
                  <td>
                    {proofLike && worst.value !== null
                      ? <span className={overLimit ? 'worst-summary worst-summary--over' : 'worst-summary'}>{positionLabel(worst.position)} · {worst.value.toFixed(2)}{tolerance !== undefined && <small> / ≤ {tolerance}</small>}</span>
                      : missing.length ? <span className="missing-tag">缺 {missing.map((position) => positionLabel(position)).join('、')}</span> : linkedProof ? <span className="missing-tag">校样未采读</span> : '-'}
                  </td>
                  <td><StatusBadge status={item.status} /></td>
                  <td>{item.relatedCode || '-'}</td>
                  <td className="evidence-cell">{item.evidence || '未上传'}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

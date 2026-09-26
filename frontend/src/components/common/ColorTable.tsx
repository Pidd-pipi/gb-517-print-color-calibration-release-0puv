
import type { DomainRecord } from '../../types/domain';
import { formatReading, judgmentLabel, positionLabel, statusTone } from '../../utils/format';
import { StatusBadge } from './StatusBadge';

function hasPositionReadings(item: DomainRecord): boolean {
  return item.readingOperator != null || item.readingMiddle != null || item.readingDrive != null || Boolean(item.judgmentResult);
}

function ReadingCell({ value, overLimit }: { value?: number | null; overLimit: boolean }) {
  if (value === null || value === undefined) return <td className="reading reading--missing">未录</td>;
  return <td className={`reading${overLimit ? ' reading--over' : ''}`}>{formatReading(value)}{overLimit && <small>超限</small>}</td>;
}

function ProofReadingTable({ records, title }: { records: DomainRecord[]; title: string }) {
  return <section className="color-panel" aria-label={title}><header><div><span className="eyebrow">COLOR EVIDENCE</span><h2>{title}</h2></div><small>操作侧/中间/传动侧三点读数，按最差位置判定</small></header><div className="color-table"><table><thead><tr><th>样本</th><th>操作侧</th><th>中间</th><th>传动侧</th><th>最差位置</th><th>当前判定</th><th>状态</th></tr></thead><tbody>{records.slice(0, 5).map((item) => {
    const overLimit = (item.overLimit || '').split(',').filter(Boolean);
    return <tr key={item.id}><td><strong>{item.code}</strong><small>{item.name}</small></td>
      <ReadingCell value={item.readingOperator} overLimit={overLimit.includes('operator')} />
      <ReadingCell value={item.readingMiddle} overLimit={overLimit.includes('middle')} />
      <ReadingCell value={item.readingDrive} overLimit={overLimit.includes('drive')} />
      <td>{item.worstPosition ? positionLabel(item.worstPosition) : '-'}</td>
      <td>{item.judgmentResult ? <><span className={`status status--${statusTone(item.judgmentResult)}`}>{judgmentLabel(item.judgmentResult)}</span><small>判定 v{item.judgmentVersion}</small></> : '-'}</td>
      <td><StatusBadge status={item.status} /></td></tr>;
  })}</tbody></table></div></section>;
}

export function ColorTable({ records, title = '色彩读数与证据' }: { records: DomainRecord[]; title?: string }) {
  if (!records.length) return null;
  if (records.some(hasPositionReadings)) return <ProofReadingTable records={records} title={title} />;
  return <section className="color-panel" aria-label={title}><header><div><span className="eyebrow">COLOR EVIDENCE</span><h2>{title}</h2></div><small>ΔE/密度读数随版本留痕</small></header><div className="color-table color-table--legacy"><table><thead><tr><th>样本</th><th>读数</th><th>状态</th><th>关联批次</th><th>证据</th></tr></thead><tbody>{records.slice(0, 5).map((item) => <tr key={item.id}><td><strong>{item.code}</strong><small>{item.name}</small></td><td>{item.metricValue} {item.metricUnit}</td><td><StatusBadge status={item.status} /></td><td>{item.relatedCode || '-'}</td><td className="evidence-cell">{item.evidence || '未上传'}</td></tr>)}</tbody></table></div></section>;
}

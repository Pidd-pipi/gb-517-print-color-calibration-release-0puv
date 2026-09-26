import type { DomainRecord } from '../../types/domain';
import { formatDate } from '../../utils/format';
import { PROOF_POSITIONS, formatReading, positionLabel, positionReading } from '../../utils/proof';

const conclusionLabel: Record<string, string> = {
  pending: '待复核',
  accepted: '复核通过',
  rejected: '复核不通过',
};

export function JudgmentHistory({ record }: { record: DomainRecord }) {
  const judgments = record.judgments ?? [];
  if (!judgments.length) return null;
  return (
    <div className="judgment-list">
      <h3>判定版本（按最差位置判定，现场以当前版本为准）</h3>
      {judgments.map((judgment) => {
        const overLimit = judgment.tolerance > 0 && judgment.worstValue > judgment.tolerance;
        const incomplete = PROOF_POSITIONS.some((position) => positionReading(judgment, position.key) === null);
        return (
          <article key={judgment.id} className={judgment.active ? 'judgment judgment--active' : 'judgment judgment--history'}>
            <header>
              <strong>
                v{judgment.judgmentNo} · {conclusionLabel[judgment.conclusion] ?? judgment.conclusion}
                {judgment.active && <span className="active-tag">本批采用</span>}
              </strong>
              <span>{judgment.actor} · {formatDate(judgment.createdAt)}</span>
            </header>
            <div className="judgment-readings">
              {PROOF_POSITIONS.map((position) => {
                const reading = positionReading(judgment, position.key);
                const isWorst = judgment.worstPosition === position.key;
                return (
                  <span key={position.key} className={isWorst && overLimit ? 'judgment-reading judgment-reading--over' : isWorst ? 'judgment-reading judgment-reading--worst' : 'judgment-reading'}>
                    {position.label}：{formatReading(reading)}
                  </span>
                );
              })}
              <span className={overLimit ? 'judgment-worst judgment-worst--over' : 'judgment-worst'}>
                最差：{judgment.worstPosition ? positionLabel(judgment.worstPosition) : '-'} {judgment.worstValue.toFixed(2)} / 允许 ≤ {judgment.tolerance.toFixed(2)}
              </span>
              {incomplete && <span className="missing-tag">位置缺录</span>}
            </div>
            <p className="judgment-reason">{judgment.reason}</p>
            {judgment.holdReason && <p className="hold-reason">滞留原因：{judgment.holdReason}</p>}
            <code>{judgment.requestId}</code>
          </article>
        );
      })}
    </div>
  );
}

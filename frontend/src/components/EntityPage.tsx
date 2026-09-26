import { useCallback, useEffect, useMemo, useState } from 'react';
import { request } from '../api/client';
import { roleAtLeast, useAuth } from '../hooks/useAuth';
import { usePagination } from '../hooks/usePagination';
import type { EntityConfig, DomainRecord } from '../types/domain';
import type { RunState } from '../types/status';
import type { EntityStore } from '../stores/factory';
import { formatDate } from '../utils/format';
import { StatusBadge } from './common/StatusBadge';
import { RunStateBadge } from './common/RunStateBadge';
import { ColorTable } from './common/ColorTable';
import { JudgmentHistory } from './common/JudgmentHistory';
import { EmptyState } from './common/EmptyState';
import { MetricCard } from './common/MetricCard';
import { ConfirmDialog } from './common/ConfirmDialog';
import { UiButton } from './common/UiButton';
import { DEFAULT_TOLERANCE, PROOF_POSITIONS, formatReading, hasAnyReading, missingPositions, positionLabel, positionReading, worstReading } from '../utils/proof';

function decisionRunState(status: string): RunState {
  if (status === 'release') return 'released';
  if (status === 'rework' || status === 'quarantine') return 'hold';
  return 'proofing';
}

function nextPermittedStatus(config: EntityConfig, current: string, reviewer: boolean): string | null {
  const transitions: Record<string, Record<string, string | null>> = {
    pressUnit: { ready: 'setup', setup: 'printing', printing: 'maintenance', maintenance: 'printing' },
    printRun: { setup: 'printing', printing: 'proofing', proofing: reviewer ? 'released' : 'hold', hold: 'proofing', released: reviewer ? 'hold' : null },
    colorProof: { captured: 'review', review: reviewer ? 'accepted' : null, accepted: reviewer ? 'review' : null, rejected: reviewer ? 'review' : null },
    releaseDecision: { draft: reviewer ? 'release' : 'rework', release: reviewer ? 'rework' : null, rework: reviewer ? 'release' : null, quarantine: reviewer ? 'rework' : null },
  };
  return transitions[config.key]?.[current] ?? null;
}

export function EntityPage({ config, useStore }: { config: EntityConfig; useStore: EntityStore }) {
  const { session } = useAuth();
  const { items, meta, loading, error, load, createRecord, transition } = useStore();
  const [search, setSearch] = useState('');
  const [submittedSearch, setSubmittedSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; status: string } | null>(null);
  const [detail, setDetail] = useState<DomainRecord | null>(null);
  const [runIndex, setRunIndex] = useState<Record<string, DomainRecord>>({});
  const [proofIndex, setProofIndex] = useState<Record<string, DomainRecord>>({});
  const { page, pageSize, pages, setPage, previous, next } = usePagination(meta.total);
  const canWrite = roleAtLeast(session?.role, 'operator');
  const canReview = roleAtLeast(session?.role, 'reviewer');

  useEffect(() => { void load(config.path, submittedSearch, page, pageSize); }, [config.path, load, page, pageSize, submittedSearch]);
  // Keep a lightweight code -> batch index so proofs can show the batch limit,
  // and a code -> proof index so the batch page can show the gate verdict.
  useEffect(() => {
    let cancelled = false;
    const needRuns = config.key === 'colorProof' || config.key === 'printRun';
    const needProofs = config.key === 'printRun';
    if (!needRuns && !needProofs) return;
    const jobs: Promise<unknown>[] = [];
    if (needRuns) jobs.push(request<DomainRecord[]>('/runs?page=1&pageSize=100').then((result) => {
      if (!cancelled) setRunIndex(Object.fromEntries(result.data.map((run) => [run.code, run])));
    }).catch(() => undefined));
    if (needProofs) jobs.push(request<DomainRecord[]>('/proofs?page=1&pageSize=100').then((result) => {
      if (!cancelled) setProofIndex(Object.fromEntries(result.data.map((proof) => [proof.relatedCode, proof])));
    }).catch(() => undefined));
    void Promise.all(jobs);
    return () => { cancelled = true; };
  }, [config.key, items.length]);
  const toleranceFor = useCallback((record: DomainRecord) => runIndex[record.relatedCode]?.colorTolerance, [runIndex]);
  const proofForRun = useCallback((runCode?: string) => (runCode ? proofIndex[runCode] : undefined), [proofIndex]);
  const highRisk = useMemo(() => items.filter((item) => ['high', 'critical'].includes(item.riskLevel)).length, [items]);
  const createDemo = async () => {
    const now = Date.now();
    const isProof = config.key === 'colorProof';
    const isRun = config.key === 'printRun';
    await createRecord(config.path, { code: `${config.key.toUpperCase()}-${now.toString().slice(-6)}`, name: `新增${config.label}`,
      description: '通过前端工作台创建的业务记录', facility: '默认作业区', owner: session?.username || 'operator', category: '常规', riskLevel: 'medium',
      metricValue: isProof ? 2.4 : undefined, metricUnit: isProof || isRun ? 'ΔE' : undefined, effectiveAt: new Date().toISOString(),
      evidence: '已完成创建前色彩检查', relatedCode: 'PR-001',
      ...(isRun ? { colorTolerance: DEFAULT_TOLERANCE } : {}),
      ...(isProof ? { operationSide: 2.1, center: 2.4, driveSide: 1.9 } : {}) });
    setShowCreate(false);
  };
  const openDetail = async (item: DomainRecord) => {
    try { setDetail((await request<DomainRecord>(`/${config.path}/${item.id}`)).data); }
    catch { setDetail(item); }
  };

  return <main className="workspace">
    <header className="page-header"><div><p className="eyebrow">业务工作台</p><h1>{config.label}</h1><p>统一管理{config.label}的状态、风险、证据与责任人。</p></div>{canWrite && <UiButton onClick={() => setShowCreate(true)}>新增{config.label}</UiButton>}</header>
    <section className="metrics"><MetricCard label="记录总数" value={meta.total} detail="当前筛选范围"/><MetricCard label="高风险" value={highRisk} detail="需要优先复核"/><MetricCard label="状态种类" value={new Set(items.map((item) => item.status)).size} detail="状态机覆盖"/></section>
    {(config.key === 'colorProof' || config.key === 'releaseDecision') && <ColorTable records={items} toleranceFor={toleranceFor} title={config.key === 'colorProof' ? '当前校样读数' : '放行依据读数'} />}
    {config.key === 'printRun' && <ColorTable records={items} proofForRun={proofForRun} title="批次校样门控（按最差位置）" />}
    {config.key === 'printRun' && items.some((item) => item.status === 'hold' && item.holdReason) && (
      <div className="hold-banner" role="alert">
        <strong>批次停留在校样阶段</strong>
        <ul>
          {items.filter((item) => item.status === 'hold' && item.holdReason).map((item) => (
            <li key={item.id}><button className="record-link" onClick={() => void openDetail(item)}>{item.code}</button>：{item.holdReason}</li>
          ))}
        </ul>
      </div>
    )}
    <section className="toolbar"><input aria-label="搜索" placeholder={`搜索${config.label}编码或名称`} value={search} onChange={(event) => setSearch(event.target.value)} /><UiButton onClick={() => { setPage(1); setSubmittedSearch(search); }}>查询</UiButton><button className="link-button" onClick={() => { setSearch(''); setSubmittedSearch(''); setPage(1); }}>重置</button></section>
    {error && <div className="alert" role="alert">{error}</div>}
    <section className="table-shell" aria-busy={loading}><table><thead><tr><th>编码</th><th>名称</th><th>状态</th><th>风险</th><th>责任人</th><th>指标</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => { const target = nextPermittedStatus(config, item.status, canReview); const proofLike = config.key === 'colorProof' && hasAnyReading(item); const tolerance = config.key === 'colorProof' ? (toleranceFor(item) ?? DEFAULT_TOLERANCE) : (item.colorTolerance ?? DEFAULT_TOLERANCE); const worst = worstReading(item); const overProof = proofLike && worst.value !== null && worst.value > tolerance; const missing = proofLike ? missingPositions(item) : []; return <tr key={item.id} className={overProof || (config.key === 'printRun' && item.status === 'hold' && item.holdReason) ? 'row-flagged' : ''}><td><strong>{item.code}</strong></td><td><button className="record-link" onClick={() => void openDetail(item)}>{item.name}</button><small>{item.facility}</small></td><td>{config.key === 'printRun' ? <RunStateBadge state={item.status as RunState}/> : <StatusBadge status={item.status}/>} {config.key === 'releaseDecision' && <RunStateBadge state={decisionRunState(item.status)}/>}</td><td>{item.riskLevel}</td><td>{item.owner}</td><td>{proofLike ? (
            <div className="proof-cell">
              <div className="proof-cell-readings">
                {PROOF_POSITIONS.map((position) => <span key={position.key} className={worst.position === position.key ? (overProof ? 'mini-reading mini-reading--over' : 'mini-reading mini-reading--worst') : 'mini-reading'} title={positionLabel(position.key)}>{positionLabel(position.key)[0]}{formatReading(positionReading(item, position.key))}</span>)}
              </div>
              <small className={overProof ? 'mini-worst mini-worst--over' : 'mini-worst'}>最差 {positionLabel(worst.position)} {worst.value?.toFixed(2) ?? '-'} ≤ {tolerance}</small>
              {(missing.length > 0 || overProof) && <small className="mini-flag">⚠ {missing.length ? `缺录：${missing.map((p) => positionLabel(p)).join('、')}` : '最差值超批次范围'}</small>}
            </div>
          ) : config.key === 'printRun' ? (
            <div className="proof-cell"><small>ΔE 允许 ≤ {item.colorTolerance ?? DEFAULT_TOLERANCE}</small>{item.holdReason && <small className="mini-flag">⚠ {item.holdReason}</small>}</div>
          ) : `${item.metricValue} ${item.metricUnit}`}</td><td>{formatDate(item.updatedAt)}</td><td>{canWrite && target ? <button className="table-action" onClick={() => setPending({ item, status: target })}>推进至 {target}</button> : <button className="table-action" onClick={() => void openDetail(item)}>查看详情</button>}</td></tr>; })}
      {!items.length && !loading && <tr><td colSpan={8}><EmptyState title="没有匹配记录" detail="可清空搜索条件后重新查询" /></td></tr>}
    </tbody></table>{loading && <div className="loading">正在同步业务数据…</div>}</section>
    <footer className="pagination"><button onClick={previous} disabled={page <= 1}>上一页</button><span>第 {page} / {pages} 页</span><button onClick={next} disabled={page >= pages}>下一页</button></footer>
    <ConfirmDialog open={showCreate} title={`新增${config.label}`} onCancel={() => setShowCreate(false)} onConfirm={() => void createDemo()}><p>将创建一条包含完整责任人、风险和证据信息的演示记录。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending)} title="确认状态迁移" onCancel={() => setPending(null)} onConfirm={() => { if (pending) void transition(config.path, pending.item, pending.status).then(() => setPending(null)); }}><p>状态迁移会写入审计日志；色彩配置和放行决定同时生成不可变版本。</p><strong>{pending?.item.status} → {pending?.status}</strong></ConfirmDialog>
    <ConfirmDialog open={Boolean(detail)} title={`${detail?.code || ''} 记录详情`} onCancel={() => setDetail(null)} onConfirm={() => setDetail(null)}>{detail && <div className="detail-content"><p>{detail.description}</p>{detail.holdReason && <div className="hold-banner hold-banner--detail" role="alert"><strong>批次停留在校样阶段</strong><span>{detail.holdReason}</span></div>}<dl><div><dt>证据</dt><dd>{detail.evidence || '-'}</dd></div><div><dt>当前版本</dt><dd>v{detail.version}</dd></div>{config.key === 'printRun' && <div><dt>批次 ΔE 允许范围</dt><dd>≤ {detail.colorTolerance ?? DEFAULT_TOLERANCE} {detail.metricUnit || 'ΔE'}</dd></div>}</dl><ColorTable records={[detail]} toleranceFor={toleranceFor} title="记录色彩读数" />{detail.judgments?.length ? <JudgmentHistory record={detail} /> : null}{detail.revisions?.length ? <div className="revision-list"><h3>版本链</h3>{detail.revisions.map((revision) => <article key={revision.id} className={revision.holdReason ? 'revision revision--hold' : ''}><strong>v{revision.version} · {revision.status}</strong><span>{revision.actor} · {revision.reason}</span>{revision.colorTolerance ? <small>ΔE 允许 ≤ {revision.colorTolerance}</small> : null}{revision.holdReason && <small className="hold-reason">滞留原因：{revision.holdReason}</small>}<code>{revision.requestId}</code></article>)}</div> : null}</div>}</ConfirmDialog>
  </main>;
}

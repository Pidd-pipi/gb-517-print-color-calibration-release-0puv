
export type ProofPosition = 'operation' | 'center' | 'drive';

export interface JudgmentRecord {
  id: number;
  colorProofId: number;
  judgmentNo: number;
  active: boolean;
  conclusion: 'pending' | 'accepted' | 'rejected';
  status: string;
  operationSide: number | null;
  center: number | null;
  driveSide: number | null;
  worstValue: number;
  worstPosition: ProofPosition | '';
  tolerance: number;
  holdReason: string;
  runCode: string;
  actor: string;
  requestId: string;
  reason: string;
  createdAt: string;
}

export interface DomainRecord {
  id: number;
  code: string;
  name: string;
  status: string;
  version: number;
  description: string;
  facility: string;
  owner: string;
  category: string;
  riskLevel: 'low' | 'medium' | 'high' | 'critical';
  metricValue: number;
  metricUnit: string;
  effectiveAt: string;
  evidence: string;
  relatedCode: string;
  createdAt: string;
  updatedAt: string;
  revisions?: RevisionRecord[];
  // 印刷批次：批次允许 ΔE 范围与校样门控滞留原因
  colorTolerance?: number;
  holdReason?: string;
  // 色彩校样：操作侧 / 中间 / 传动侧三位置读数
  operationSide?: number | null;
  center?: number | null;
  driveSide?: number | null;
  worstPosition?: ProofPosition | '';
  judgments?: JudgmentRecord[];
}

export interface RevisionRecord {
  id: number; version: number; status: string; name: string; metricValue: number;
  metricUnit: string; evidence: string; actor: string; requestId: string; reason: string;
  colorTolerance?: number; holdReason?: string; createdAt: string;
}

export interface PageMeta { page: number; pageSize: number; total: number }
export interface ApiEnvelope<T> { data: T; error?: string; message?: string; meta?: PageMeta }
export interface UserSession { token: string; username: string; displayName: string; role: string; expiresIn: number }
export interface AuditLog {
  id: number; requestId: string; actor: string; action: string; entityType: string;
  entityId: number; beforeState: string; afterState: string; detail: string; createdAt: string;
}
export interface EntityConfig { key: string; path: string; label: string; statuses: readonly string[] }

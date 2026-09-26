
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
  // 三位置校样读数（操作侧/中间/传动侧）；null 表示该位置未录。
  readingOperator?: number | null;
  readingMiddle?: number | null;
  readingDrive?: number | null;
  // 当前生效的判定结论（来自最新判定版本的冗余字段）。
  judgmentVersion?: number;
  judgmentResult?: string;
  judgmentReason?: string;
  worstPosition?: string;
  overLimit?: string;
  judgments?: JudgmentRecord[];
  // 批次字段：允许色差上限与校样滞留原因。
  deltaELimit?: number;
  holdReason?: string;
}

export interface JudgmentRecord {
  id: number;
  version: number;
  result: string;
  reason: string;
  readingOperator?: number | null;
  readingMiddle?: number | null;
  readingDrive?: number | null;
  worstValue: number;
  worstPosition: string;
  tolerance: number;
  overLimit?: string;
  actor: string;
  requestId: string;
  createdAt: string;
}

export interface RevisionRecord {
  id: number; version: number; status: string; name: string; metricValue: number;
  metricUnit: string; evidence: string; actor: string; requestId: string; reason: string; createdAt: string;
}

export interface PageMeta { page: number; pageSize: number; total: number }
export interface ApiEnvelope<T> { data: T; error?: string; message?: string; meta?: PageMeta }
export interface UserSession { token: string; username: string; displayName: string; role: string; expiresIn: number }
export interface AuditLog {
  id: number; requestId: string; actor: string; action: string; entityType: string;
  entityId: number; beforeState: string; afterState: string; detail: string; createdAt: string;
}
export interface EntityConfig { key: string; path: string; label: string; statuses: readonly string[] }

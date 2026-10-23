import type { ManualKnowledgePayload } from '@/api/knowledge-base'
import type { SourceVersion, SourceVersionAssetKind } from '@/api/research'

export interface SourceItem {
  id: string; knowledge_base_id?: string; title?: string; file_name?: string; file_type?: string;
  type?: string; source?: string; parse_status?: string; summary_status?: string;
  error_message?: string; created_at?: string; updated_at?: string; custom_metadata?: Record<string, unknown>;
}
export function sourceState(item: SourceItem, searchable = true): { label: string; tone: string } {
  const status = item.parse_status || ''
  if (status === 'archived') return { label: '原图已归档 · 未识别', tone: 'pending' }
  if (['completed', 'success'].includes(status)) return { label: searchable ? '可检索' : '已整理', tone: 'success' }
  if (['failed', 'error'].includes(status)) return { label: '需要处理', tone: 'danger' }
  if (['processing', 'running', 'parsing'].includes(status)) return { label: '正在整理', tone: 'processing' }
  if (['pending', 'queued'].includes(status)) return { label: '已保存 · 排队中', tone: 'pending' }
  if (status === 'draft') return { label: '已保存 · 待发布', tone: 'pending' }
  if (status === 'cancelled') return { label: '已取消整理', tone: 'pending' }
  return { label: '已接收', tone: 'pending' }
}
interface SourceCollection {
  embedding_model_id?: string
  indexing_strategy?: { vector_enabled?: boolean; keyword_enabled?: boolean; wiki_enabled?: boolean }
}
export function quickNotePublishingReady(kb?: SourceCollection): boolean {
  if (!kb) return false
  // Mirrors KnowledgeBase.NeedsEmbeddingModel: plain parsed sources do not
  // require an embedder or summary model, and can still support draft writing.
  const strategy = kb.indexing_strategy
  const needsEmbedding = !strategy || strategy.vector_enabled || strategy.keyword_enabled
  return !needsEmbedding || !!kb.embedding_model_id
}
export function collectionSearchEnabled(kb?: SourceCollection): boolean {
  if (!kb) return false
  const strategy = kb.indexing_strategy
  return !strategy || !!(strategy.vector_enabled || strategy.keyword_enabled || strategy.wiki_enabled)
}
export function errorMessage(error: unknown): string {
  const value = error as { status?: number; message?: string; error?: string | { message?: string } }
  if (value?.status === 409) return value.message || '内容或权限已变化，请刷新后重试。你的编辑内容仍保留。'
  return value?.message || (typeof value?.error === 'string' ? value.error : value?.error?.message) || '操作失败，请检查网络或服务状态后重试。'
}
export function exactMemberIds(members: { tenant_id: number }[]): number[] {
  return [...new Set(members.map(item => item.tenant_id).filter(id => Number.isSafeInteger(id) && id > 0))].sort((a, b) => a - b)
}
export function chatSourceScope(kbId: string, ids: string[]) {
  return { knowledgeBases: ids.length ? [] : [kbId], files: [...new Set(ids)], fileKbMap: Object.fromEntries(ids.map(id => [id, kbId])) }
}
export function quickNotePayload(title: string, content: string, modelConfigured: boolean): ManualKnowledgePayload {
  if (!title.trim() || !content.trim()) throw new Error('请填写笔记标题和正文。')
  return { title: title.trim(), content, status: modelConfigured ? 'publish' : 'draft' }
}
export function sourceVersionFilename(version: SourceVersion, kind: SourceVersionAssetKind, fallback = '来源'): string {
  const name = (version.file_name || fallback).split(/[\\/]/).pop() || '来源'
  const suffix = version.id.slice(0, 8)
  if (kind === 'original') {
    const dot = name.lastIndexOf('.')
    const extension = dot > 0 ? name.slice(dot) : /^[a-z0-9]+$/i.test(version.file_type || '') ? `.${version.file_type}` : ''
    return `${dot > 0 ? name.slice(0, dot) : name}-${suffix}${extension}`
  }
  return `${name}-${suffix}.${kind === 'markdown' ? 'md' : 'html.txt'}`
}
export const terminalJob = (status: string) => ['completed', 'ready', 'failed', 'partial'].includes(status)
export const jobLabel = (status: string) => ({ queued: '等待处理', processing: '处理中', completed: '完成', ready: '可下载', partial: '部分失败', failed: '失败', saved: '已保存', moved: '已归集 · 待授权', shared: '已共享' }[status] || status)

/** An org response is usable only for the current request and selected org. */
export function isCurrentAudience(request: number, current: number, requestedOrg: string, currentOrg: string): boolean {
  return request === current && requestedOrg === currentOrg
}
export function completedShareSince(previous: { id: string; kind: string; status: string }[], next: { id: string; kind: string; status: string }[]): boolean {
  const states = new Map(previous.map(job => [job.id, job.status]))
  return next.some(job => job.kind === 'share' && terminalJob(job.status) && states.has(job.id) && !terminalJob(states.get(job.id)!))
}

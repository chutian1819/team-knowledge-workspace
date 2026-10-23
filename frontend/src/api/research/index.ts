import { get, post, put, getDown } from '@/utils/request'
import type { WikiPage } from '@/api/wiki'
import { sourceVersionAssetUrl, type SourceVersionAssetKind } from './sourceVersionAsset'
import { sourceReplacementRequest } from './sourceReplacement'
import { uploadTimeoutMs } from '@/utils/requestTimeouts'
export type { SourceVersionAssetKind } from './sourceVersionAsset'

export interface Envelope<T> { success: boolean; data: T }
export interface BookmarkItem { url: string; title: string; folder: string; added_at?: string }
export interface CaptureJobItem extends BookmarkItem { knowledge_id?: string; status: string; error?: string }
export interface CaptureJob {
  id: string; kind: string; status: string; total: number; completed: number; failed: number;
  items: CaptureJobItem[]; error?: string; created_at?: string;
}
export interface ResearchTemplate { id: string; name: string; content: string }
export interface CreateDraftPayload { title: string; template_id?: string; content?: string; source_ids: string[]; source_scope?: '' | 'all' | 'selected' | 'retrieval'; source_kb_id?: string; retrieval_query?: string; retrieval_kb_id?: string }
export interface RetrievalEvidence {
  version: number; query: string; knowledge_base_id: string; embedding_model_id: string; mode: string; captured_at: string;
  results: { knowledge_id: string; chunk_id: string; chunk_type: string; content: string; content_sha256: string; rank: number; score: number }[];
}
export interface Proposal {
  id: string; page_id: string; base_version: number; base_content: string; content: string;
  status: string; source_refs?: string[]; created_at?: string;
  instruction?: string; writing_rules?: WritingRuleSnapshot[];
  source_versions?: Record<string, unknown> & { _retrieval?: RetrievalEvidence };
}
export interface WritingRuleSnapshot { id: string; version: number; name: string; instruction: string }
export interface WritingRule {
  id: string; knowledge_base_id: string; name: string; instruction: string; template_id: string;
  status: 'candidate' | 'active' | 'revoked'; version: number;
  source_page_id?: string; source_proposal_id?: string; source_event_id?: string; created_by?: string; created_at?: string; updated_at?: string;
}
export interface WritingEvent {
  id: string; page_id: string; kind: string; instruction?: string;
  base_version: number; result_version?: number; base_content?: string; content?: string;
  proposal_id?: string; model_id?: string; status?: string; error?: string; created_by?: string; created_at?: string;
  learning_status?: string; learning_error?: string;
}
export interface WritingLearning { enabled: boolean; model_id: string; status?: string; last_error?: string }
export type WritingRating = 'useful' | 'useless' | 'misapplied'
export interface WritingFeedback {
  rule_id: string; proposal_id: string; rating: WritingRating; reason: string; created_by: string; is_mine?: boolean; updated_at?: string;
}
export interface ExportJob { id: string; page_id?: string; status: string; format?: string; error?: string; filename?: string; file_name?: string }
const capture = '/api/v1/research/capture'
const research = (kbId: string) => `/api/v1/knowledgebase/${encodeURIComponent(kbId)}/research`
const draft = (kbId: string, id: string) => `${research(kbId)}/drafts/${encodeURIComponent(id)}`

export const ensureInbox = (templateKbId?: string) => post<Envelope<{ id: string; name: string }>>(`${capture}/inbox`, { template_kb_id: templateKbId })
export const getInbox = () => get<Envelope<{ id: string; name: string } | null>>(`${capture}/inbox`)
export const previewBookmarks = (html: string) => post<Envelope<{ items: BookmarkItem[]; skipped: number }>>(`${capture}/bookmarks/preview`, { html })
export const importBookmarks = (kbId: string, items: BookmarkItem[], key: string) => post<Envelope<CaptureJob>>(`${capture}/${kbId}/bookmarks/import`, { items, idempotency_key: key })
export const listCaptureJobs = (kbId: string) => get<Envelope<CaptureJob[]>>(`${capture}/${kbId}/jobs`)
export const retryCaptureJob = (kbId: string, jobId: string) => post<Envelope<CaptureJob>>(`${capture}/${kbId}/jobs/${jobId}/retry`)
export const shareSources = (kbId: string, data: { target_kb_id: string; organization_id: string; selected_tenant_ids: number[]; knowledge_ids: string[]; idempotency_key: string }) => post<Envelope<CaptureJob>>(`${capture}/${kbId}/share`, data)
export const listTemplates = (kbId: string) => get<Envelope<ResearchTemplate[]>>(`${research(kbId)}/templates`)
export const listDrafts = (kbId: string) => get<Envelope<WikiPage[]>>(`${research(kbId)}/drafts`)
export const createDraft = (kbId: string, data: CreateDraftPayload) => post<Envelope<WikiPage>>(`${research(kbId)}/drafts`, data)
export const getDraft = (kbId: string, id: string) => get<Envelope<WikiPage>>(draft(kbId, id))
export const saveDraft = (kbId: string, id: string, data: { base_version: number; title: string; content: string }) => put<Envelope<WikiPage>>(draft(kbId, id), data)
export const listProposals = (kbId: string, id: string) => get<Envelope<Proposal[]>>(`${draft(kbId, id)}/proposals`)
export const createProposal = (kbId: string, id: string, data: { base_version: number; instruction: string; model_id: string; writing_rule_ids?: string[]; retrieval_query?: string; retrieval_kb_id?: string }) => post<Envelope<Proposal>>(`${draft(kbId, id)}/proposals`, data, { timeout: 180000 })
export const listWritingEvents = (kbId: string, id: string, beforeId?: string) => get<Envelope<WritingEvent[]>>(`${draft(kbId, id)}/writing-events${beforeId ? `?before_id=${encodeURIComponent(beforeId)}` : ''}`)
export const getWritingEvent = (kbId: string, id: string, eventId: string) => get<Envelope<WritingEvent>>(`${draft(kbId, id)}/writing-events/${encodeURIComponent(eventId)}`)
export const getWritingLearning = (kbId: string, id: string) => get<Envelope<WritingLearning>>(`${draft(kbId, id)}/writing-learning`)
export const saveWritingLearning = (kbId: string, id: string, data: { enabled: boolean; model_id: string }) => post<Envelope<WritingLearning>>(`${draft(kbId, id)}/writing-learning`, data)
export const extractWritingEvent = (kbId: string, id: string, eventId: string, data: { model_id: string }) => post<Envelope<WritingRule[]>>(`${draft(kbId, id)}/writing-events/${encodeURIComponent(eventId)}/extract`, data, { timeout: 180000 })
export const listWritingRules = (kbId: string) => get<Envelope<WritingRule[]>>(`${research(kbId)}/writing-rules`)
export const createWritingRule = (kbId: string, data: { name: string; instruction: string; template_id: string; source_proposal_id?: string }) => post<Envelope<WritingRule>>(`${research(kbId)}/writing-rules`, data)
export const decideWritingRule = (kbId: string, ruleId: string, data: { base_version: number; status: 'active' | 'revoked'; name?: string; instruction?: string }) => post<Envelope<WritingRule>>(`${research(kbId)}/writing-rules/${encodeURIComponent(ruleId)}/decide`, data)
export const extractWritingRules = (kbId: string, id: string, data: { proposal_id: string; model_id: string }) => post<Envelope<WritingRule[]>>(`${draft(kbId, id)}/writing-rules/extract`, data, { timeout: 180000 })
export const listWritingFeedback = (kbId: string, id: string, proposalId: string) => get<Envelope<WritingFeedback[]>>(`${draft(kbId, id)}/proposals/${encodeURIComponent(proposalId)}/writing-feedback`)
export const saveWritingFeedback = (kbId: string, id: string, proposalId: string, data: { rule_id: string; rating: WritingRating; reason: string }) => post<Envelope<WritingFeedback>>(`${draft(kbId, id)}/proposals/${encodeURIComponent(proposalId)}/writing-feedback`, data)
export const acceptProposal = (kbId: string, id: string, proposalId: string) => post<Envelope<WikiPage>>(`${draft(kbId, id)}/proposals/${proposalId}/accept`)
export const rejectProposal = (kbId: string, id: string, proposalId: string) => post<Envelope<Proposal>>(`${draft(kbId, id)}/proposals/${proposalId}/reject`)
export const listDraftRevisions = (kbId: string, id: string) => get<any>(`${draft(kbId, id)}/revisions`)
export const revertDraft = (kbId: string, id: string, version: number, baseVersion: number) => post<Envelope<WikiPage>>(`${draft(kbId, id)}/revert`, { version, base_version: baseVersion })
export const exportDraft = (kbId: string, id: string, format: string) => post<Envelope<ExportJob>>(`${draft(kbId, id)}/exports`, { format })
export const getDraftExport = (kbId: string, exportId: string) => get<Envelope<ExportJob>>(`${research(kbId)}/exports/${exportId}`)
export const downloadDraftExport = (kbId: string, exportId: string) => getDown(`${research(kbId)}/exports/${exportId}/download`)

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename.replace(/[\\/:*?"<>|]/g, '_')
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export interface SourceVersion {
  id: string
  knowledge_id: string
  /** SHA-256 of the original bytes for files, extracted text for URLs. */
  content_hash: string
  source_url?: string
  /** Locator is intentionally private; originals use the version asset endpoint. */
  has_original?: boolean
  file_name?: string
  file_type?: string
  /** Native upload checksum (MD5), distinct from the immutable version hash. */
  file_hash?: string
  snapshot_path?: string
  markdown_path?: string
  image_paths?: string[]
  captured_at: string
}
export const listSourceVersions = (kbId: string, knowledgeId: string) => get<Envelope<SourceVersion[]>>(`${capture}/${kbId}/sources/${knowledgeId}`)
export const downloadSourceVersionFile = (kbId: string, knowledgeId: string, versionId: string, kind: SourceVersionAssetKind) => getDown(sourceVersionAssetUrl(kbId, knowledgeId, versionId, kind))
export const replaceSourceFile = (kbId: string, knowledgeId: string, file: File, onUploadProgress?: (event: { loaded: number; total?: number }) => void) => {
  const request = sourceReplacementRequest(kbId, knowledgeId, file)
  return put<Envelope<{ id: string; parse_status?: string; file_name?: string; file_type?: string }>>(request.url, request.data, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: uploadTimeoutMs(request.data),
    onUploadProgress,
  })
}
export const exportProjectAssets = (kbId: string) => post<Envelope<ExportJob>>(`${research(kbId)}/asset-exports`, {})
export const listExports = (kbId: string, pageId?: string) => get<Envelope<ExportJob[]>>(`${research(kbId)}/exports${pageId ? `?page_id=${encodeURIComponent(pageId)}` : ''}`)

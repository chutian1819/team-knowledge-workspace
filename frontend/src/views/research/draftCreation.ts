import type { CreateDraftPayload } from '@/api/research'

export type DraftSourceScope = 'template' | 'selected' | 'all' | 'retrieval'

export function defaultDraftSourceScope(selectedIds: string[]): DraftSourceScope {
  return selectedIds.length ? 'selected' : 'template'
}

export function canCreateWithScope(scope: DraftSourceScope, selectedIds: string[]): boolean {
  return scope !== 'selected' || selectedIds.length > 0
}

export function draftCreationPayload(title: string, templateId: string, scope: DraftSourceScope, selectedIds: string[], sourceKbId: string, retrievalQuery = ''): CreateDraftPayload {
  if (!canCreateWithScope(scope, selectedIds)) throw new Error('请先勾选资料，或选择仅建立模板。')
  if (scope === 'retrieval' && (!sourceKbId || !retrievalQuery.trim())) throw new Error('请选择来源知识库并填写检索问题。')
  return {
    title: title.trim(),
    template_id: templateId,
    source_scope: scope === 'template' ? '' : scope,
    source_ids: scope === 'selected' ? [...selectedIds] : [],
    ...(scope === 'template' ? {} : { source_kb_id: sourceKbId }),
    ...(scope === 'retrieval' ? { retrieval_query: retrievalQuery.trim(), retrieval_kb_id: sourceKbId } : {}),
  }
}

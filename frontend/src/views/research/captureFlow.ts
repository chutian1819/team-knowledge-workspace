import type { SourceItem } from './workspaceState'

export interface CapturedSource { success: boolean; data: SourceItem }
export type ExistingSource = SourceItem & { knowledge_base_id: string }

/** request.ts rejects with the native JSON body spread onto { status, message }. */
export function existingSourceFromConflict(error: unknown): ExistingSource | null {
  const value = error as { status?: number; code?: string; data?: SourceItem } | null
  if (value?.status !== 409 || !['duplicate_file', 'duplicate_url'].includes(value.code || '')) return null
  const source = value.data
  if (!source || typeof source.id !== 'string' || !source.id.trim()
    || typeof source.knowledge_base_id !== 'string' || !source.knowledge_base_id.trim()) return null
  return source as ExistingSource
}

/**
 * A duplicate settles only that input and never enters the metadata write path.
 * Real errors stop the batch; settled callbacks remove only confirmed inputs.
 */
export async function collectSources<T>(
  items: T[],
  upload: (item: T) => Promise<CapturedSource>,
  handlers: {
    settled: (item: T) => void
    created: (response: CapturedSource) => Promise<void>
    existing: (source: ExistingSource) => void
  },
): Promise<{ created: number; existing: number }> {
  const counts = { created: 0, existing: 0 }
  for (const item of items) {
    let response: CapturedSource
    try { response = await upload(item) }
    catch (error) {
      const existing = existingSourceFromConflict(error)
      if (!existing) throw error
      handlers.settled(item)
      handlers.existing(existing)
      counts.existing++
      continue
    }
    if (!response?.success || !response.data?.id) throw new Error('服务未确认资料保存成功，未保存的输入仍保留，请重试。')
    handlers.settled(item)
    await handlers.created(response)
    counts.created++
  }
  return counts
}

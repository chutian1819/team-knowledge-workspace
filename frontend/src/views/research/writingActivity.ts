import type { WritingEvent } from '@/api/research'

export function canExtractWritingEvent(event: WritingEvent): boolean {
  return ['save', 'accept', 'revert'].includes(event.kind) && (event.base_content || '') !== (event.content || '')
}

/** An empty prompt result means no output, rather than deletion of the draft. */
export function writingEventContentChange(event: WritingEvent): { before: string; after: string } | null {
  if (event.kind === 'prompt' || event.kind === 'generation_failed') return null
  const before = event.base_content || ''
  const after = event.content || ''
  return before === after ? null : { before, after }
}

/** Preserve the server's cursor order and previously loaded history on refresh. */
export function mergeWritingEventPage(current: WritingEvent[], page: WritingEvent[], older = false): WritingEvent[] {
  const incoming = new Map(page.map(event => [event.id, event]))
  const known = new Set(current.map(event => event.id))
  if (older) {
    return [...current.map(event => incoming.get(event.id) || event), ...page.filter(event => !known.has(event.id))]
  }
  // A full new page without overlap leaves a gap. Restart its cursor window
  // so loading older pages cannot skip the unseen interval.
  if (current.length && page.length >= 100 && !page.some(event => known.has(event.id))) return [...page]
  return [...page, ...current.filter(event => !incoming.has(event.id))]
}

export function writingEventName(kind: string): string {
  return ({ create: '创建文稿', save: '保存正文', prompt: '提交写作要求', proposal: '生成修改提案', generation_failed: '生成失败', accept: '接受修改', reject: '拒绝修改', revert: '恢复版本' } as Record<string, string>)[kind] || kind
}

export function writingLearningStatus(status?: string): string {
  return ({ queued: '等待提炼', pending: '等待提炼', running: '正在提炼', processing: '正在提炼', completed: '提炼完成', complete: '提炼完成', succeeded: '提炼完成', failed: '提炼失败', idle: '等待下一次正文修改' } as Record<string, string>)[status || ''] || status || '等待下一次正文修改'
}

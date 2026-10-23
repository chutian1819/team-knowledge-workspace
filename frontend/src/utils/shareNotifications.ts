import type { SharedKnowledgeBase } from '@/api/organization'

/** Only currently authorized shares are displayed; withdrawn shares disappear. */
export function shareNotificationItems(shares: SharedKnowledgeBase[], readIds: string[]) {
  const read = new Set(readIds)
  return [...new Map(shares.filter(item => item.share_id && item.knowledge_base?.id)
    .map(item => [item.share_id, item])).values()]
    .sort((a, b) => (Date.parse(b.shared_at) || 0) - (Date.parse(a.shared_at) || 0) || a.share_id.localeCompare(b.share_id))
    .map(item => ({ ...item, read: read.has(item.share_id) }))
}

export function sharedKnowledgeLocation(item: SharedKnowledgeBase) {
  return item.knowledge_base.type === 'faq'
    ? { path: `/platform/knowledge-bases/${encodeURIComponent(item.knowledge_base.id)}` }
    : { path: '/platform/research', query: { section: 'team', kb: item.knowledge_base.id, project: item.organization_id } }
}

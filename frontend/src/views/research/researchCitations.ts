/** Turn only native knowledge citations into inert local anchors before sanitizing Markdown. */
export function localizeResearchCitations(markdown: string): string {
  return markdown.replace(/knowledge:\/\/([a-zA-Z0-9_-]+)(?:\/chunks\/([a-zA-Z0-9_-]+))?/g,
    (_, knowledgeId, chunkId) => `#research-source/${knowledgeId}${chunkId ? `/${chunkId}` : ''}`)
}
export function parseResearchCitation(href: string): { knowledgeId: string; chunkId?: string } | null {
  const match = /^#research-source\/([a-zA-Z0-9_-]+)(?:\/([a-zA-Z0-9_-]+))?$/.exec(href)
  return match ? { knowledgeId: match[1]!, chunkId: match[2] } : null
}
export function researchSourceRefs(refs: string[]): { id: string; title: string }[] {
  return refs.map(ref => { const [id, ...title] = ref.split('|'); return { id: id!, title: title.join('|') || id! } })
    .filter(ref => /^[a-zA-Z0-9_-]+$/.test(ref.id))
}

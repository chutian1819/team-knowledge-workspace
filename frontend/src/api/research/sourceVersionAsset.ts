export type SourceVersionAssetKind = 'snapshot' | 'markdown' | 'original'

export function sourceVersionAssetUrl(kbId: string, knowledgeId: string, versionId: string, kind: SourceVersionAssetKind): string {
  return `/api/v1/knowledgebase/${encodeURIComponent(kbId)}/research/sources/${encodeURIComponent(knowledgeId)}/versions/${encodeURIComponent(versionId)}/assets/${kind}`
}

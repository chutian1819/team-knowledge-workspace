export function sourceReplacementRequest(kbId: string, knowledgeId: string, file: File) {
  const data = new FormData()
  data.append('file', file, file.name)
  return {
    url: `/api/v1/research/capture/${encodeURIComponent(kbId)}/sources/${encodeURIComponent(knowledgeId)}/file`,
    data,
  }
}

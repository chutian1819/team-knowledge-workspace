interface MemberResponse<Member> {
  success: boolean
  data?: { members: Member[] }
  message?: string
}

interface MemberRequestOptions<Member> {
  request: (organizationId: string) => Promise<MemberResponse<Member>>
  setMembers: (members: Member[]) => void
  setError: (error: string | null) => void
  setLoading: (loading: boolean) => void
}

/** Only the latest member request may update the active organization panel. */
export function createOrganizationMemberRequest<Member>(options: MemberRequestOptions<Member>) {
  let revision = 0
  let organizationId: string | null = null

  const invalidate = () => {
    revision++
    organizationId = null
    options.setLoading(false)
  }

  const setContext = (nextId: string | null) => {
    if (organizationId !== null && organizationId !== nextId) {
      invalidate()
      options.setMembers([])
    }
  }

  const fetch = async (nextId: string): Promise<Member[]> => {
    const requestRevision = ++revision
    if (organizationId !== nextId) options.setMembers([])
    organizationId = nextId
    options.setLoading(true)
    options.setError(null)
    try {
      const response = await options.request(nextId)
      if (requestRevision !== revision) return []
      if (response.success && response.data) {
        options.setMembers(response.data.members)
        return response.data.members
      }
      options.setError(response.message || 'Failed to fetch members')
      return []
    } catch (error) {
      if (requestRevision === revision) {
        options.setError(error instanceof Error ? error.message : 'Failed to fetch members')
      }
      return []
    } finally {
      if (requestRevision === revision) options.setLoading(false)
    }
  }

  const matchesContext = (id: string) => organizationId === id

  return { fetch, invalidate, setContext, matchesContext }
}

// SPDX-License-Identifier: AGPL-3.0-or-later
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import signatureService, {
  type Signature,
  type SignatureStatus,
  type CreateSignatureRequest,
  type PendingDocument,
} from '@/services/signatures'

export const useSignatureStore = defineStore('signatures', () => {
  const userSignatures = ref<Signature[]>([])
  const pendingDocuments = ref<PendingDocument[]>([])
  const documentSignatures = ref<Map<string, Signature[]>>(new Map())
  const signatureStatuses = ref<Map<string, SignatureStatus>>(new Map())
  const loading = ref(false)
  const loadingPending = ref(false)
  const error = ref<string | null>(null)

  const getUserSignaturesCount = computed(() => userSignatures.value.length)

  const getDocumentSignatures = computed(() => {
    return (docId: string) => documentSignatures.value.get(docId) || []
  })

  const getSignatureStatus = computed(() => {
    return (docId: string) => signatureStatuses.value.get(docId)
  })

  const isDocumentSigned = computed(() => {
    return (docId: string) => {
      const status = signatureStatuses.value.get(docId)
      return status?.isSigned || false
    }
  })

  async function createSignature(request: CreateSignatureRequest): Promise<Signature> {
    loading.value = true
    error.value = null

    try {
      const signature = await signatureService.createSignature(request)

      if (!userSignatures.value.find((s: Signature) => s.id === signature.id)) {
        userSignatures.value.unshift(signature)
      }

      const docSigs = documentSignatures.value.get(request.docId) || []
      if (!docSigs.find((s: Signature) => s.id === signature.id)) {
        documentSignatures.value.set(request.docId, [signature, ...docSigs])
      }

      signatureStatuses.value.set(request.docId, {
        docId: signature.docId,
        userEmail: signature.userEmail,
        isSigned: true,
        signedAt: signature.signedAt,
      })

      return signature
    } catch (err: any) {
      error.value = err.response?.data?.error?.message || 'Failed to create signature'
      throw err
    } finally {
      loading.value = false
    }
  }

  async function fetchUserSignatures(): Promise<void> {
    loading.value = true
    error.value = null

    try {
      const signatures = await signatureService.getUserSignatures()
      userSignatures.value = signatures
    } catch (err: any) {
      error.value = err.response?.data?.error?.message || 'Failed to fetch signatures'
      throw err
    } finally {
      loading.value = false
    }
  }

  async function fetchPendingDocuments(): Promise<void> {
    loadingPending.value = true
    try {
      pendingDocuments.value = await signatureService.getMyPendingDocuments()
    } catch (err: any) {
      error.value = err.response?.data?.error?.message || 'Failed to fetch pending documents'
    } finally {
      loadingPending.value = false
    }
  }

  async function fetchDocumentSignatures(docId: string): Promise<Signature[]> {
    loading.value = true
    error.value = null

    try {
      const signatures = await signatureService.getDocumentSignatures(docId)
      documentSignatures.value.set(docId, signatures)
      return signatures
    } catch (err: any) {
      error.value = err.response?.data?.error?.message || 'Failed to fetch document signatures'
      throw err
    } finally {
      loading.value = false
    }
  }

  async function fetchSignatureStatus(docId: string): Promise<SignatureStatus> {
    loading.value = true
    error.value = null

    try {
      const status = await signatureService.getSignatureStatus(docId)
      signatureStatuses.value.set(docId, status)
      return status
    } catch (err: any) {
      error.value = err.response?.data?.error?.message || 'Failed to fetch signature status'
      throw err
    } finally {
      loading.value = false
    }
  }

  async function checkUserSigned(docId: string): Promise<boolean> {
    try {
      const status = await fetchSignatureStatus(docId)
      return status.isSigned
    } catch (err) {
      return false
    }
  }

  function clearError(): void {
    error.value = null
  }

  function clearCache(): void {
    userSignatures.value = []
    pendingDocuments.value = []
    documentSignatures.value.clear()
    signatureStatuses.value.clear()
    error.value = null
  }

  return {
    userSignatures,
    pendingDocuments,
    documentSignatures,
    signatureStatuses,
    loading,
    loadingPending,
    error,
    getUserSignaturesCount,
    getDocumentSignatures,
    getSignatureStatus,
    isDocumentSigned,
    createSignature,
    fetchUserSignatures,
    fetchPendingDocuments,
    fetchDocumentSignatures,
    fetchSignatureStatus,
    checkUserSigned,
    clearError,
    clearCache,
  }
})

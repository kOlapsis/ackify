// SPDX-License-Identifier: AGPL-3.0-or-later
import http, { type ApiResponse } from './http'

export interface ServiceInfo {
  name: string
  icon: string
  type: string
  referrer: string
}

export interface Signature {
  id: number
  docId: string
  userSub: string
  userEmail: string
  userName?: string
  signedAt: string
  payloadHash: string
  signature: string
  nonce: string
  createdAt: string
  referer?: string
  prevHash?: string
  serviceInfo?: ServiceInfo
  // Document metadata
  docTitle?: string
  docUrl?: string
  docDeletedAt?: string
}

export interface SignatureStatus {
  docId: string
  userEmail: string
  isSigned: boolean
  signedAt?: string
}

export interface CreateSignatureRequest {
  docId: string
  referer?: string
}

export interface CreateSignatureResponse {
  id: number
  docId: string
  userEmail: string
  signedAt: string
}

/**
 * Signature service for managing document signatures
 */
export interface PendingDocument {
  docId: string
  title: string
  addedAt: string
}

export const signatureService = {
  /**
   * Create a signature for a document
   */
  async createSignature(request: CreateSignatureRequest): Promise<Signature> {
    const response = await http.post<ApiResponse<Signature>>('/signatures', request)
    return response.data.data
  },

  /**
   * Get current user's signatures
   */
  async getUserSignatures(): Promise<Signature[]> {
    const response = await http.get<ApiResponse<Signature[]>>('/signatures')
    return response.data.data
  },

  /**
   * Get signatures for a specific document
   */
  async getDocumentSignatures(docId: string): Promise<Signature[]> {
    const response = await http.get<ApiResponse<Signature[]>>(`/documents/${docId}/signatures`)
    return response.data.data
  },

  /**
   * Get signature status for a document (current user)
   */
  async getSignatureStatus(docId: string): Promise<SignatureStatus> {
    const response = await http.get<ApiResponse<SignatureStatus>>(
      `/documents/${docId}/signatures/status`
    )
    return response.data.data
  },

  /**
   * Check if user has signed a document
   */
  async getMyPendingDocuments(): Promise<PendingDocument[]> {
    const response = await http.get<ApiResponse<PendingDocument[]>>('/users/me/pending-documents')
    return response.data.data
  },

  async hasUserSigned(docId: string): Promise<boolean> {
    try {
      const status = await this.getSignatureStatus(docId)
      return status.isSigned
    } catch (error) {
      return false
    }
  },
}

export default signatureService

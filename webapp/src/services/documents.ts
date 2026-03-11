// SPDX-License-Identifier: AGPL-3.0-or-later
import axios from 'axios'
import http, { type ApiResponse, API_BASE } from './http'

export interface CreateDocumentRequest {
  reference: string
  title?: string
}

export interface UploadDocumentResponse {
  doc_id: string
  title: string
  storage_key: string
  storage_provider: string
  file_size: number
  mime_type: string
  checksum: string
  checksum_algorithm: string
  created_at: string
  is_new: boolean
}

export interface UploadProgress {
  loaded: number
  total: number
  percent: number
}

export interface UploadDocumentOptions {
  title?: string
  readMode?: 'integrated' | 'external'
  allowDownload?: boolean
  requireFullRead?: boolean
  verifyChecksum?: boolean
}

export interface CreateDocumentResponse {
  docId: string
  url?: string
  title: string
  createdAt: string
}

export interface FindOrCreateDocumentResponse {
  docId: string
  url?: string
  title: string
  checksum?: string
  checksumAlgorithm?: string
  description?: string
  readMode: 'external' | 'integrated'
  allowDownload: boolean
  requireFullRead: boolean
  verifyChecksum: boolean
  createdAt: string
  isNew: boolean // true if created, false if found
  signatureCount: number
  // Storage fields for uploaded documents
  storageKey?: string
  mimeType?: string
}

// MyDocument represents a document in the user's document list
export interface MyDocument {
  id: string
  title: string
  url?: string
  description: string
  createdAt: string
  updatedAt: string
  signatureCount: number
  expectedSignerCount: number
  totalSignatureCount: number
}

// PaginatedResponse for paginated API responses
export interface PaginatedResponse<T> {
  data: T[]
  meta: {
    page: number
    pageSize: number
    total: number
    totalPages: number
  }
}

/**
 * Document service for managing documents
 */
export const documentService = {
  /**
   * Create a new document with a unique ID
   */
  async createDocument(request: CreateDocumentRequest): Promise<CreateDocumentResponse> {
    const response = await http.post<ApiResponse<CreateDocumentResponse>>('/documents', request)
    return response.data.data
  },

  /**
   * Find an existing document by reference or create it if not found
   * @param reference URL, path, or document ID
   * @returns Document information with isNew flag
   */
  async findOrCreateDocument(reference: string): Promise<FindOrCreateDocumentResponse> {
    const response = await http.get<ApiResponse<FindOrCreateDocumentResponse>>(
      '/documents/find-or-create',
      { params: { doc: reference } }
    )
    return response.data.data
  },

  /**
   * List documents created by the current user
   * @param limit Number of documents per page (default: 20)
   * @param page Page number (1-indexed)
   * @param search Optional search query
   * @returns Paginated list of user's documents
   */
  async listMyDocuments(
    limit = 20,
    page = 1,
    search?: string
  ): Promise<PaginatedResponse<MyDocument>> {
    const params: Record<string, any> = { limit, page }
    if (search && search.trim()) {
      params.search = search.trim()
    }

    const response = await http.get<{
      data: MyDocument[]
      success: boolean
      meta: { page: number; pageSize: number; total: number; totalPages: number }
    }>('/users/me/documents', { params })

    return {
      data: response.data.data,
      meta: response.data.meta
    }
  },

  /**
   * Delete a document by ID (uses owner endpoint, works for both admin and owner)
   * @param docId Document ID to delete
   */
  async deleteDocument(docId: string): Promise<void> {
    await http.delete(`/users/me/documents/${docId}`)
  },

  /**
   * Upload a file and create a document
   * @param file File to upload
   * @param options Upload options including title and reader settings
   * @param onProgress Optional callback for upload progress
   * @returns Upload response with document info
   */
  async uploadDocument(
    file: File,
    options?: UploadDocumentOptions,
    onProgress?: (progress: UploadProgress) => void
  ): Promise<UploadDocumentResponse> {
    // Get CSRF token first
    const csrfResponse = await axios.get(`${API_BASE}/csrf`, { withCredentials: true })
    const csrfToken = csrfResponse.data.data?.token || csrfResponse.data.token

    const formData = new FormData()
    formData.append('file', file)
    if (options?.title) {
      formData.append('title', options.title)
    }
    if (options?.readMode) {
      formData.append('readMode', options.readMode)
    }
    if (options?.allowDownload !== undefined) {
      formData.append('allowDownload', String(options.allowDownload))
    }
    if (options?.requireFullRead !== undefined) {
      formData.append('requireFullRead', String(options.requireFullRead))
    }
    if (options?.verifyChecksum !== undefined) {
      formData.append('verifyChecksum', String(options.verifyChecksum))
    }

    const response = await axios.post<ApiResponse<UploadDocumentResponse>>(
      `${API_BASE}/documents/upload`,
      formData,
      {
        withCredentials: true,
        headers: {
          'Content-Type': 'multipart/form-data',
          'X-CSRF-Token': csrfToken,
        },
        onUploadProgress: (progressEvent) => {
          if (onProgress && progressEvent.total) {
            const percent = Math.round((progressEvent.loaded * 100) / progressEvent.total)
            onProgress({
              loaded: progressEvent.loaded,
              total: progressEvent.total,
              percent,
            })
          }
        },
      }
    )

    return response.data.data
  },
}

export default documentService

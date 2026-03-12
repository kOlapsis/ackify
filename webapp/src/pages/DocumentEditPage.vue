<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { usePageTitle } from '@/composables/usePageTitle'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'
import {
  getDocumentStatus,
  updateDocumentMetadata,
  addExpectedSigner,
  removeExpectedSigner,
  sendReminders,
  deleteDocument,
  previewCSVSigners,
  importSigners,
  type DocumentStatus,
  type CSVPreviewResult,
  type CSVSignerEntry,
} from '@/services/admin'
import { extractError } from '@/services/http'
import { useConfigStore } from '@/stores/config'
import SignersSection from '@/components/SignersSection.vue'
import RemindersSection from '@/components/RemindersSection.vue'
import {
  ArrowLeft,
  CheckCircle,
  Loader2,
  Copy,
  X,
  Trash2,
  AlertCircle,
  AlertTriangle,
  ChevronRight,
  ExternalLink,
  Check,
  FileText,
  FileCheck,
  FileX,
  Eye,
  Download,
  ScrollText,
  ShieldCheck,
  Users,
  Clock,
} from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const configStore = useConfigStore()
const { t, locale } = useI18n()

// Data
const docId = computed(() => route.params.id as string)
usePageTitle('documentEdit.title', { docId: docId.value })
const documentStatus = ref<DocumentStatus | null>(null)
const loading = ref(true)
const error = ref('')
const success = ref('')
const unauthorized = ref(false)

// Modals
const showAddSignersModal = ref(false)
const showDeleteConfirmModal = ref(false)
const showRemoveSignerModal = ref(false)
const showSendRemindersModal = ref(false)
const showImportCSVModal = ref(false)
const signerToRemove = ref('')
const remindersMessage = ref('')

// CSV Import
const csvFile = ref<File | null>(null)
const csvPreview = ref<CSVPreviewResult | null>(null)
const analyzingCSV = ref(false)
const importingCSV = ref(false)
const csvError = ref('')

// Metadata form
const metadataForm = ref<Partial<{
  title: string
  url: string
  checksum: string
  checksumAlgorithm: string
  description: string
  readMode: string
  allowDownload: boolean
  requireFullRead: boolean
  verifyChecksum: boolean
}>>({
  title: '',
  url: '',
  checksum: '',
  checksumAlgorithm: 'SHA-256',
  description: '',
  readMode: 'integrated',
  allowDownload: true,
  requireFullRead: false,
  verifyChecksum: true,
})
const savingMetadata = ref(false)

// Expected signers form
const signersEmails = ref('')
const addingSigners = ref(false)

// Reminders
const sendMode = ref<'all' | 'selected'>('all')
const selectedEmails = ref<string[]>([])
const sendingReminders = ref(false)

// Delete
const deletingDocument = ref(false)

// Copy feedback
const copied = ref(false)

// Computed
const shareLink = computed(() => {
  if (!documentStatus.value) return ''
  return documentStatus.value.shareLink
})

const stats = computed(() => documentStatus.value?.stats ?? null)
const reminderStats = computed(() => documentStatus.value?.reminderStats)
const smtpEnabled = computed(() => configStore.smtpEnabled)
const expectedSigners = computed(() => documentStatus.value?.expectedSigners || [])
const documentMetadata = computed(() => documentStatus.value?.document)
const documentTitle = computed(() => documentMetadata.value?.title || docId.value)
const isStoredDocument = computed(() => !!documentMetadata.value?.storageKey)
const unexpectedSignatures = computed(() => documentStatus.value?.unexpectedSignatures || [])
const isAdmin = computed(() => authStore.isAdmin)

// Methods
async function loadDocumentStatus() {
  try {
    loading.value = true
    error.value = ''
    unauthorized.value = false
    const useOwnerEndpoint = !authStore.isAdmin
    const response = await getDocumentStatus(docId.value, useOwnerEndpoint)
    documentStatus.value = response.data

    // Check authorization
    if (!authStore.isAdmin && documentStatus.value.document?.createdBy !== authStore.user?.email) {
      unauthorized.value = true
      return
    }

    // Pre-fill metadata form if document exists
    if (documentStatus.value.document) {
      const doc = documentStatus.value.document
      metadataForm.value = {
        title: doc.title || '',
        url: doc.url || '',
        checksum: doc.checksum || '',
        checksumAlgorithm: doc.checksumAlgorithm || 'SHA-256',
        description: doc.description || '',
        readMode: doc.readMode || 'integrated',
        allowDownload: doc.allowDownload ?? true,
        requireFullRead: doc.requireFullRead ?? false,
        verifyChecksum: doc.verifyChecksum ?? true,
      }
    }
  } catch (err: any) {
    if (err?.response?.status === 403 || err?.response?.status === 404) {
      unauthorized.value = true
    } else {
      error.value = extractError(err)
    }
    console.error('Failed to load document status:', err)
  } finally {
    loading.value = false
  }
}

async function saveMetadata() {
  try {
    savingMetadata.value = true
    error.value = ''
    success.value = ''
    const useOwnerEndpoint = !authStore.isAdmin
    await updateDocumentMetadata(docId.value, metadataForm.value, useOwnerEndpoint)
    success.value = t('documentEdit.metadataSaved')
    await loadDocumentStatus()
    setTimeout(() => (success.value = ''), 3000)
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to save metadata:', err)
  } finally {
    savingMetadata.value = false
  }
}

async function addSigners() {
  if (!signersEmails.value.trim()) return

  try {
    addingSigners.value = true
    error.value = ''
    success.value = ''

    const lines = signersEmails.value.split('\n').filter(l => l.trim())
    let addedCount = 0

    for (const line of lines) {
      const trimmed = line.trim()
      const match = trimmed.match(/^(.+?)\s*<(.+?)>$/)
      const email = match && match[2] ? match[2].trim() : trimmed
      const name = match && match[1] ? match[1].trim() : ''

      try {
        const useOwnerEndpoint = !authStore.isAdmin
        await addExpectedSigner(docId.value, { email, name }, useOwnerEndpoint)
        addedCount++
      } catch (err) {
        console.error(`Failed to add ${email}:`, err)
      }
    }

    showAddSignersModal.value = false
    signersEmails.value = ''
    success.value = t('documentEdit.signersAdded', { count: addedCount })
    await loadDocumentStatus()
    setTimeout(() => (success.value = ''), 3000)
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to add signers:', err)
  } finally {
    addingSigners.value = false
  }
}

function confirmRemoveSigner(email: string) {
  signerToRemove.value = email
  showRemoveSignerModal.value = true
}

async function removeSigner() {
  const email = signerToRemove.value
  if (!email) return

  try {
    error.value = ''
    success.value = ''
    const useOwnerEndpoint = !authStore.isAdmin
    await removeExpectedSigner(docId.value, email, useOwnerEndpoint)
    success.value = t('documentEdit.signerRemoved', { email })
    showRemoveSignerModal.value = false
    signerToRemove.value = ''
    await loadDocumentStatus()
    setTimeout(() => (success.value = ''), 3000)
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to remove signer:', err)
  }
}

function handleReminderSend(mode: 'all' | 'selected') {
  sendMode.value = mode
  remindersMessage.value =
    mode === 'all'
      ? t('documentEdit.confirmSendReminders', { count: reminderStats.value?.pendingCount || 0 })
      : t('documentEdit.confirmSendRemindersSelected', { count: selectedEmails.value.length })
  showSendRemindersModal.value = true
}

async function sendRemindersAction() {
  try {
    sendingReminders.value = true
    error.value = ''
    success.value = ''

    const normalizedLocale = locale.value.split('-')[0]
    const response = await sendReminders(
      docId.value,
      {
        emails: sendMode.value === 'selected' ? selectedEmails.value : undefined,
      },
      normalizedLocale
    )

    selectedEmails.value = []
    showSendRemindersModal.value = false

    if (response.data.result) {
      const result = response.data.result
      if (result.failed > 0) {
        success.value = t('documentEdit.remindersSentPartial', { sent: result.successfullySent, failed: result.failed })
      } else {
        success.value = t('documentEdit.remindersSentSuccess', { count: result.successfullySent })
      }
    } else {
      success.value = t('documentEdit.remindersSentGeneric')
    }

    await loadDocumentStatus()
    setTimeout(() => (success.value = ''), 3000)
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to send reminders:', err)
  } finally {
    sendingReminders.value = false
  }
}

async function copyToClipboard() {
  try {
    await navigator.clipboard.writeText(shareLink.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}

function formatDate(dateString: string | undefined): string {
  if (!dateString) return 'N/A'
  const date = new Date(dateString)
  return date.toLocaleDateString(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

async function handleDeleteDocument() {
  try {
    deletingDocument.value = true
    error.value = ''
    const useOwnerEndpoint = !authStore.isAdmin
    await deleteDocument(docId.value, useOwnerEndpoint)
    showDeleteConfirmModal.value = false
    router.push('/documents')
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to delete document:', err)
    showDeleteConfirmModal.value = false
  } finally {
    deletingDocument.value = false
  }
}

// CSV Import functions (admin only)
function openImportCSVModal() {
  csvFile.value = null
  csvPreview.value = null
  csvError.value = ''
  showImportCSVModal.value = true
}

function handleCSVFileChange(event: Event) {
  const target = event.target as HTMLInputElement
  if (target.files && target.files[0]) {
    csvFile.value = target.files[0]
    csvPreview.value = null
    csvError.value = ''
  }
}

async function analyzeCSV() {
  if (!csvFile.value) return

  try {
    analyzingCSV.value = true
    csvError.value = ''
    const response = await previewCSVSigners(docId.value, csvFile.value)
    csvPreview.value = response.data
  } catch (err) {
    csvError.value = extractError(err)
    console.error('Failed to analyze CSV:', err)
  } finally {
    analyzingCSV.value = false
  }
}

function getSignerStatus(signer: CSVSignerEntry): 'valid' | 'exists' {
  if (!csvPreview.value) return 'valid'
  return csvPreview.value.existingEmails.includes(signer.email) ? 'exists' : 'valid'
}

const signersToImport = computed(() => {
  if (!csvPreview.value) return []
  return csvPreview.value.signers.filter(
    s => !csvPreview.value!.existingEmails.includes(s.email)
  )
})

async function confirmImportCSV() {
  if (!csvPreview.value || signersToImport.value.length === 0) return

  try {
    importingCSV.value = true
    csvError.value = ''

    const signersData = signersToImport.value.map(s => ({
      email: s.email,
      name: s.name
    }))

    const response = await importSigners(docId.value, signersData)

    showImportCSVModal.value = false
    csvFile.value = null
    csvPreview.value = null

    success.value = t('admin.documentDetail.csvImportSuccess', {
      imported: response.data.imported,
      skipped: response.data.skipped
    })
    await loadDocumentStatus()
    setTimeout(() => (success.value = ''), 3000)
  } catch (err) {
    csvError.value = extractError(err)
    console.error('Failed to import signers:', err)
  } finally {
    importingCSV.value = false
  }
}

function closeImportCSVModal() {
  showImportCSVModal.value = false
  csvFile.value = null
  csvPreview.value = null
  csvError.value = ''
}

watch(() => metadataForm.value.readMode, (newMode) => {
  if (newMode === 'external') {
    metadataForm.value.requireFullRead = false
    metadataForm.value.allowDownload = false
  }
})

onMounted(async () => {
  if (!authStore.initialized) {
    await authStore.checkAuth()
  }
  loadDocumentStatus()
})
</script>

<template>
  <div class="min-h-[calc(100vh-8rem)]">
    <main class="mx-auto max-w-6xl px-4 sm:px-6 py-6 sm:py-8">
      <!-- Breadcrumb -->
      <nav class="flex items-center gap-2 text-sm mb-6">
        <router-link to="/documents" class="text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 transition-colors">
          {{ t('documentEdit.breadcrumb.myDocuments') }}
        </router-link>
        <ChevronRight :size="16" class="text-slate-300 dark:text-slate-600" />
        <span class="text-slate-900 dark:text-slate-100 font-medium truncate max-w-[200px]">{{ documentTitle }}</span>
      </nav>

      <!-- Page Header -->
      <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-6 sm:mb-8">
        <div class="flex items-start gap-4">
          <div class="w-12 h-12 sm:w-14 sm:h-14 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center flex-shrink-0">
            <FileText class="w-6 h-6 sm:w-7 sm:h-7 text-blue-600 dark:text-blue-400" />
          </div>
          <div>
            <h1 class="text-xl sm:text-2xl font-bold text-slate-900 dark:text-white">{{ documentTitle }}</h1>
            <p class="text-sm text-slate-500 dark:text-slate-400 mt-1">{{ t('documentEdit.subtitle') }}</p>
          </div>
        </div>
        <button
          @click="router.push('/documents')"
          class="w-full sm:w-auto inline-flex items-center justify-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 font-medium rounded-lg px-4 py-2.5 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors min-h-[44px]"
        >
          <ArrowLeft :size="18" />
          {{ t('common.back') }}
        </button>
      </div>

      <!-- Alerts -->
      <div v-if="error" class="mb-6 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
        <div class="flex items-start">
          <AlertCircle :size="20" class="mr-3 mt-0.5 text-red-600 dark:text-red-400 flex-shrink-0" />
          <p class="text-sm text-red-700 dark:text-red-300">{{ error }}</p>
        </div>
      </div>

      <div v-if="success" class="mb-6 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-xl p-4">
        <div class="flex items-start">
          <CheckCircle :size="20" class="mr-3 mt-0.5 text-emerald-600 dark:text-emerald-400 flex-shrink-0" />
          <p class="text-sm text-emerald-700 dark:text-emerald-300">{{ success }}</p>
        </div>
      </div>

      <!-- Loading -->
      <div v-if="loading" class="flex flex-col items-center justify-center py-24">
        <Loader2 :size="48" class="animate-spin text-blue-600" />
        <p class="mt-4 text-slate-500 dark:text-slate-400">{{ t('common.loading') }}</p>
      </div>

      <!-- Unauthorized -->
      <div v-else-if="unauthorized" class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-8 text-center">
        <div class="w-16 h-16 mx-auto bg-red-100 dark:bg-red-900/30 rounded-2xl flex items-center justify-center mb-4">
          <AlertCircle :size="32" class="text-red-600 dark:text-red-400" />
        </div>
        <h2 class="text-xl font-semibold text-slate-900 dark:text-slate-100 mb-2">{{ t('documentEdit.unauthorized.title') }}</h2>
        <p class="text-slate-500 dark:text-slate-400 mb-6">{{ t('documentEdit.unauthorized.description') }}</p>
        <router-link
          to="/documents"
          class="inline-flex items-center gap-2 trust-gradient text-white font-medium rounded-lg px-6 py-2.5 hover:opacity-90 transition-opacity"
        >
          <ArrowLeft :size="16" />
          {{ t('documentEdit.unauthorized.backToDocuments') }}
        </router-link>
      </div>

      <!-- Document Content -->
      <div v-else-if="documentStatus" class="space-y-6">
        <!-- Share Link Card -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
          <h2 class="font-semibold text-slate-900 dark:text-slate-100 mb-4">{{ t('documentEdit.shareLink.title') }}</h2>
          <div class="flex flex-col sm:flex-row gap-3">
            <div class="flex-1 relative">
              <input
                type="text"
                :value="shareLink"
                readonly
                class="w-full px-4 py-2.5 pr-10 rounded-lg border border-slate-200 dark:border-slate-600 bg-slate-50 dark:bg-slate-700 text-slate-900 dark:text-slate-100 text-sm font-mono"
              />
              <a
                :href="shareLink"
                target="_blank"
                class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600 dark:hover:text-slate-300"
              >
                <ExternalLink :size="16" />
              </a>
            </div>
            <button
              @click="copyToClipboard"
              class="inline-flex items-center justify-center gap-2 bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors"
            >
              <Check v-if="copied" :size="16" class="text-emerald-500" />
              <Copy v-else :size="16" />
              {{ copied ? t('documentEdit.shareLink.copied') : t('documentEdit.shareLink.copy') }}
            </button>
          </div>
        </div>

        <!-- Stats Cards -->
        <div v-if="stats && stats.expectedCount > 0" class="grid gap-4 grid-cols-2 lg:grid-cols-3">
          <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-4">
            <div class="flex items-center gap-3">
              <div class="w-10 h-10 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center flex-shrink-0">
                <Users :size="20" class="text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <p class="text-xs text-slate-500 dark:text-slate-400">{{ t('documentEdit.stats.expected') }}</p>
                <p class="text-xl font-bold text-slate-900 dark:text-slate-100">{{ stats.expectedCount }}</p>
              </div>
            </div>
          </div>

          <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-4">
            <div class="flex items-center gap-3">
              <div class="w-10 h-10 rounded-xl bg-emerald-50 dark:bg-emerald-900/30 flex items-center justify-center flex-shrink-0">
                <CheckCircle :size="20" class="text-emerald-600 dark:text-emerald-400" />
              </div>
              <div>
                <p class="text-xs text-slate-500 dark:text-slate-400">{{ t('documentEdit.stats.confirmed') }}</p>
                <p class="text-xl font-bold text-slate-900 dark:text-slate-100">{{ stats.signedCount }}</p>
              </div>
            </div>
          </div>

          <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-4">
            <div class="flex items-center gap-3">
              <div class="w-10 h-10 rounded-xl bg-amber-50 dark:bg-amber-900/30 flex items-center justify-center flex-shrink-0">
                <Clock :size="20" class="text-amber-600 dark:text-amber-400" />
              </div>
              <div>
                <p class="text-xs text-slate-500 dark:text-slate-400">{{ t('documentEdit.stats.pending') }}</p>
                <p class="text-xl font-bold text-slate-900 dark:text-slate-100">{{ stats.pendingCount }}</p>
              </div>
            </div>
          </div>
        </div>

        <!-- Document Metadata -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700">
          <div class="p-6 border-b border-slate-100 dark:border-slate-700">
            <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('documentEdit.metadata.title') }}</h2>
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{{ t('documentEdit.metadata.description') }}</p>
          </div>
          <div class="p-6">
            <form @submit.prevent="saveMetadata" class="space-y-4">
              <div :class="['grid gap-4', isStoredDocument ? 'grid-cols-1' : 'grid-cols-1 md:grid-cols-2']">
                <div>
                  <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('documentEdit.metadata.titleLabel') }}</label>
                  <input v-model="metadataForm.title" :placeholder="t('documentEdit.metadata.titlePlaceholder')" class="w-full px-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent" />
                </div>
                <div v-if="!isStoredDocument">
                  <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('documentEdit.metadata.urlLabel') }}</label>
                  <input v-model="metadataForm.url" type="url" :placeholder="t('documentEdit.metadata.urlPlaceholder')" class="w-full px-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent" />
                </div>
              </div>

              <div class="grid grid-cols-1 md:grid-cols-[1fr_auto] gap-4">
                <div>
                  <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('documentEdit.metadata.checksumLabel') }}</label>
                  <input v-model="metadataForm.checksum" :placeholder="t('documentEdit.metadata.checksumPlaceholder')" class="w-full px-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent" />
                </div>
                <div class="md:min-w-[140px]">
                  <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('documentEdit.metadata.algorithmLabel') }}</label>
                  <select v-model="metadataForm.checksumAlgorithm" class="w-full px-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent">
                    <option value="SHA-256">SHA-256</option>
                    <option value="SHA-512">SHA-512</option>
                    <option value="MD5">MD5</option>
                  </select>
                </div>
              </div>

              <div>
                <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('documentEdit.metadata.descriptionLabel') }}</label>
                <textarea v-model="metadataForm.description" rows="4" :placeholder="t('documentEdit.metadata.descriptionPlaceholder')" class="w-full px-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent resize-none"></textarea>
              </div>

              <!-- Reader Options -->
              <div class="pt-4 border-t border-slate-100 dark:border-slate-700">
                <h3 class="text-sm font-medium text-slate-700 dark:text-slate-300 mb-3">{{ t('documentCreateForm.readMode.label') }}</h3>

                <!-- Read mode -->
                <div class="flex gap-4 mb-4">
                  <label class="flex items-center gap-2 cursor-pointer">
                    <input type="radio" v-model="metadataForm.readMode" value="integrated" class="w-4 h-4 text-blue-600 border-slate-300 focus:ring-blue-500" />
                    <Eye class="w-4 h-4 text-slate-500" />
                    <span class="text-sm text-slate-700 dark:text-slate-300">{{ t('documentCreateForm.readMode.integrated') }}</span>
                  </label>
                  <label class="flex items-center gap-2 cursor-pointer">
                    <input type="radio" v-model="metadataForm.readMode" value="external" class="w-4 h-4 text-blue-600 border-slate-300 focus:ring-blue-500" />
                    <ExternalLink class="w-4 h-4 text-slate-500" />
                    <span class="text-sm text-slate-700 dark:text-slate-300">{{ t('documentCreateForm.readMode.external') }}</span>
                  </label>
                </div>

                <!-- Integrated mode options -->
                <div v-if="metadataForm.readMode === 'integrated'" class="pl-4 border-l-2 border-blue-200 dark:border-blue-800 space-y-3 mb-4">
                  <label class="flex items-center gap-2 cursor-pointer">
                    <input type="checkbox" v-model="metadataForm.allowDownload" class="w-4 h-4 text-blue-600 border-slate-300 rounded focus:ring-blue-500" />
                    <Download class="w-4 h-4 text-slate-500" />
                    <span class="text-sm text-slate-700 dark:text-slate-300">{{ t('documentCreateForm.options.allowDownload') }}</span>
                  </label>
                  <label class="flex items-center gap-2 cursor-pointer">
                    <input type="checkbox" v-model="metadataForm.requireFullRead" class="w-4 h-4 text-blue-600 border-slate-300 rounded focus:ring-blue-500" />
                    <ScrollText class="w-4 h-4 text-slate-500" />
                    <span class="text-sm text-slate-700 dark:text-slate-300">{{ t('documentCreateForm.options.requireFullRead') }}</span>
                  </label>
                </div>

                <!-- Verify checksum -->
                <label class="flex items-center gap-2 cursor-pointer">
                  <input type="checkbox" v-model="metadataForm.verifyChecksum" class="w-4 h-4 text-blue-600 border-slate-300 rounded focus:ring-blue-500" />
                  <ShieldCheck class="w-4 h-4 text-slate-500" />
                  <span class="text-sm text-slate-700 dark:text-slate-300">{{ t('documentCreateForm.options.verifyChecksum') }}</span>
                </label>
              </div>

              <div v-if="documentMetadata" class="text-xs text-slate-500 dark:text-slate-400 pt-2 border-t border-slate-100 dark:border-slate-700">
                {{ t('documentEdit.metadata.createdBy', { by: documentMetadata.createdBy, date: formatDate(documentMetadata.createdAt) }) }}
              </div>

              <div class="flex justify-end">
                <button type="submit" :disabled="savingMetadata" class="trust-gradient text-white font-medium rounded-lg px-6 py-2.5 text-sm hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-2">
                  <Loader2 v-if="savingMetadata" :size="16" class="animate-spin" />
                  {{ savingMetadata ? t('documentEdit.metadata.saving') : t('common.save') }}
                </button>
              </div>
            </form>
          </div>
        </div>

        <!-- Expected Readers -->
        <SignersSection
          :expected-signers="expectedSigners"
          :unexpected-signatures="unexpectedSignatures"
          :stats="stats"
          :show-import-c-s-v="isAdmin"
          :selected-emails="selectedEmails"
          @add-signer="showAddSignersModal = true"
          @remove-signer="confirmRemoveSigner"
          @import-csv="openImportCSVModal"
          @selection-change="(emails) => selectedEmails = emails"
        />

        <!-- Email Reminders -->
        <RemindersSection
          v-if="reminderStats && stats && stats.expectedCount > 0 && (smtpEnabled || reminderStats.totalSent > 0)"
          :reminder-stats="reminderStats"
          :smtp-enabled="smtpEnabled"
          :selected-emails-count="selectedEmails.length"
          :sending="sendingReminders"
          @send="handleReminderSend"
        />

        <!-- Danger Zone -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-red-200 dark:border-red-800/50">
          <div class="p-6 border-b border-red-100 dark:border-red-800/30">
            <h2 class="font-semibold text-red-600 dark:text-red-400">{{ t('documentEdit.danger.title') }}</h2>
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{{ t('documentEdit.danger.description') }}</p>
          </div>
          <div class="p-6">
            <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 p-4 bg-red-50 dark:bg-red-900/20 rounded-xl">
              <div>
                <h3 class="font-semibold text-slate-900 dark:text-slate-100 mb-1">{{ t('documentEdit.danger.deleteDocument') }}</h3>
                <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('documentEdit.danger.deleteDescription') }}</p>
              </div>
              <button @click="showDeleteConfirmModal = true" class="inline-flex items-center justify-center gap-2 bg-red-600 hover:bg-red-700 text-white font-medium rounded-lg px-4 py-2.5 text-sm transition-colors flex-shrink-0">
                <Trash2 :size="16" />
                {{ t('common.delete') }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- Add Signers Modal -->
    <div v-if="showAddSignersModal" class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" @click.self="showAddSignersModal = false">
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 max-w-2xl w-full max-h-[90vh] overflow-auto">
        <div class="p-6 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
          <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('documentEdit.addSigners.title') }}</h2>
          <button @click="showAddSignersModal = false" class="p-2 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors">
            <X :size="20" class="text-slate-400" />
          </button>
        </div>
        <div class="p-6">
          <form @submit.prevent="addSigners" class="space-y-4">
            <div>
              <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('documentEdit.addSigners.emailsLabel') }}</label>
              <textarea v-model="signersEmails" rows="8" :placeholder="t('documentEdit.addSigners.emailsPlaceholder')" class="w-full px-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent resize-none"></textarea>
              <p class="text-xs text-slate-500 dark:text-slate-400 mt-2">{{ t('documentEdit.addSigners.emailsHelper') }}</p>
            </div>
            <div class="flex justify-end space-x-3">
              <button type="button" @click="showAddSignersModal = false" class="bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors">{{ t('common.cancel') }}</button>
              <button type="submit" :disabled="addingSigners || !signersEmails.trim()" class="trust-gradient text-white font-medium rounded-lg px-4 py-2.5 text-sm hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-2">
                <Loader2 v-if="addingSigners" :size="16" class="animate-spin" />
                {{ addingSigners ? t('documentEdit.addSigners.adding') : t('documentEdit.addSigners.add') }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>

    <!-- Delete Confirmation Modal -->
    <div v-if="showDeleteConfirmModal" class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" @click.self="showDeleteConfirmModal = false">
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-red-200 dark:border-red-800 max-w-md w-full">
        <div class="p-6 border-b border-red-100 dark:border-red-800/30 flex items-center justify-between">
          <h2 class="font-semibold text-red-600 dark:text-red-400">{{ t('documentEdit.deleteConfirm.title') }}</h2>
          <button @click="showDeleteConfirmModal = false" class="p-2 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors">
            <X :size="20" class="text-slate-400" />
          </button>
        </div>
        <div class="p-6 space-y-4">
          <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
            <p class="font-semibold text-red-900 dark:text-red-200 mb-2">{{ t('documentEdit.deleteConfirm.warning') }}</p>
            <p class="text-sm text-red-700 dark:text-red-300">{{ t('documentEdit.deleteConfirm.message') }}</p>
          </div>

          <div class="flex justify-end space-x-3 pt-4">
            <button type="button" @click="showDeleteConfirmModal = false" class="bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors">{{ t('common.cancel') }}</button>
            <button @click="handleDeleteDocument" :disabled="deletingDocument" class="bg-red-600 hover:bg-red-700 text-white font-medium rounded-lg px-4 py-2.5 text-sm transition-colors disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-2">
              <Trash2 v-if="!deletingDocument" :size="16" />
              <Loader2 v-else :size="16" class="animate-spin" />
              {{ deletingDocument ? t('documentEdit.deleteConfirm.deleting') : t('documentEdit.deleteConfirm.confirm') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Remove Signer Modal -->
    <div v-if="showRemoveSignerModal" class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" @click.self="showRemoveSignerModal = false">
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 max-w-md w-full">
        <div class="p-6 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
          <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('documentEdit.removeSigner.title') }}</h2>
          <button @click="showRemoveSignerModal = false" class="p-2 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors">
            <X :size="20" class="text-slate-400" />
          </button>
        </div>
        <div class="p-6 space-y-4">
          <p class="text-sm text-slate-600 dark:text-slate-400">{{ t('documentEdit.removeSigner.message', { email: signerToRemove }) }}</p>
          <div class="flex justify-end space-x-3 pt-4">
            <button type="button" @click="showRemoveSignerModal = false" class="bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors">{{ t('common.cancel') }}</button>
            <button @click="removeSigner" class="bg-red-600 hover:bg-red-700 text-white font-medium rounded-lg px-4 py-2.5 text-sm transition-colors">{{ t('common.delete') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Send Reminders Modal -->
    <div v-if="showSendRemindersModal" class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" @click.self="showSendRemindersModal = false">
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 max-w-md w-full">
        <div class="p-6 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
          <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('documentEdit.sendReminders.title') }}</h2>
          <button @click="showSendRemindersModal = false" class="p-2 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors">
            <X :size="20" class="text-slate-400" />
          </button>
        </div>
        <div class="p-6 space-y-4">
          <p class="text-sm text-slate-600 dark:text-slate-400">{{ remindersMessage }}</p>
          <div class="flex justify-end space-x-3 pt-4">
            <button type="button" @click="showSendRemindersModal = false" class="bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors">{{ t('common.cancel') }}</button>
            <button @click="sendRemindersAction" :disabled="sendingReminders" class="trust-gradient text-white font-medium rounded-lg px-4 py-2.5 text-sm hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-2">
              <Loader2 v-if="sendingReminders" :size="16" class="animate-spin" />
              {{ t('common.confirm') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Import CSV Modal (Admin only) -->
    <div v-if="isAdmin && showImportCSVModal" class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4" @click.self="closeImportCSVModal">
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 max-w-3xl w-full max-h-[90vh] overflow-auto">
        <div class="p-6 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
          <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('admin.documentDetail.importCSVTitle') }}</h2>
          <button @click="closeImportCSVModal" class="p-2 rounded-lg hover:bg-slate-100 dark:hover:bg-slate-700 transition-colors">
            <X :size="20" class="text-slate-400" />
          </button>
        </div>
        <div class="p-6">
          <div v-if="csvError" class="mb-4 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
            <p class="text-sm text-red-700 dark:text-red-300">{{ csvError }}</p>
          </div>

          <div v-if="!csvPreview" class="space-y-4">
            <div>
              <label class="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">{{ t('admin.documentDetail.selectFile') }}</label>
              <input type="file" accept=".csv" @change="handleCSVFileChange" class="block w-full text-sm text-slate-500 file:mr-4 file:py-2 file:px-4 file:rounded-lg file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-600 hover:file:bg-blue-100 dark:file:bg-blue-900/30 dark:file:text-blue-400 cursor-pointer" />
              <p class="text-xs text-slate-500 dark:text-slate-400 mt-2">{{ t('admin.documentDetail.csvFormatHelp') }}</p>
            </div>
            <div class="flex justify-end space-x-3">
              <button type="button" @click="closeImportCSVModal" class="bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors">{{ t('common.cancel') }}</button>
              <button @click="analyzeCSV" :disabled="!csvFile || analyzingCSV" class="trust-gradient text-white font-medium rounded-lg px-4 py-2.5 text-sm hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-2">
                <Loader2 v-if="analyzingCSV" :size="16" class="animate-spin" />
                {{ analyzingCSV ? t('admin.documentDetail.analyzing') : t('admin.documentDetail.analyze') }}
              </button>
            </div>
          </div>

          <div v-else class="space-y-4">
            <div class="grid gap-3 grid-cols-1 sm:grid-cols-3">
              <div class="bg-emerald-50 dark:bg-emerald-900/20 rounded-xl p-4 flex items-center gap-3">
                <FileCheck :size="24" class="text-emerald-600 dark:text-emerald-400" />
                <div>
                  <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('admin.documentDetail.validEntries') }}</p>
                  <p class="text-xl font-bold text-emerald-600 dark:text-emerald-400">{{ signersToImport.length }}</p>
                </div>
              </div>
              <div v-if="csvPreview.existingEmails.length > 0" class="bg-amber-50 dark:bg-amber-900/20 rounded-xl p-4 flex items-center gap-3">
                <AlertTriangle :size="24" class="text-amber-600 dark:text-amber-400" />
                <div>
                  <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('admin.documentDetail.existingEntries') }}</p>
                  <p class="text-xl font-bold text-amber-600 dark:text-amber-400">{{ csvPreview.existingEmails.length }}</p>
                </div>
              </div>
              <div v-if="csvPreview.invalidCount > 0" class="bg-red-50 dark:bg-red-900/20 rounded-xl p-4 flex items-center gap-3">
                <FileX :size="24" class="text-red-600 dark:text-red-400" />
                <div>
                  <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('admin.documentDetail.invalidEntries') }}</p>
                  <p class="text-xl font-bold text-red-600 dark:text-red-400">{{ csvPreview.invalidCount }}</p>
                </div>
              </div>
            </div>

            <div class="border border-slate-200 dark:border-slate-700 rounded-xl overflow-hidden">
              <div class="max-h-64 overflow-auto">
                <table class="w-full text-sm">
                  <thead class="bg-slate-50 dark:bg-slate-700/50">
                    <tr>
                      <th class="px-4 py-2 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">{{ t('admin.documentDetail.lineNumber') }}</th>
                      <th class="px-4 py-2 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">{{ t('admin.documentDetail.email') }}</th>
                      <th class="px-4 py-2 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">{{ t('admin.documentDetail.name') }}</th>
                      <th class="px-4 py-2 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase">{{ t('admin.documentDetail.status') }}</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-slate-100 dark:divide-slate-700">
                    <tr v-for="signer in csvPreview.signers" :key="signer.lineNumber" :class="getSignerStatus(signer) === 'exists' ? 'bg-amber-50/50 dark:bg-amber-900/10' : ''">
                      <td class="px-4 py-2 text-slate-500 dark:text-slate-400">{{ signer.lineNumber }}</td>
                      <td class="px-4 py-2 text-slate-900 dark:text-slate-100">{{ signer.email }}</td>
                      <td class="px-4 py-2 text-slate-500 dark:text-slate-400">{{ signer.name || '-' }}</td>
                      <td class="px-4 py-2">
                        <span :class="['inline-flex items-center px-2 py-0.5 text-xs font-medium rounded-full', getSignerStatus(signer) === 'exists' ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400' : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400']">
                          {{ getSignerStatus(signer) === 'exists' ? t('admin.documentDetail.statusExists') : t('admin.documentDetail.statusValid') }}
                        </span>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="flex justify-between items-center pt-4">
              <button type="button" @click="csvPreview = null; csvFile = null" class="text-sm text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100 transition-colors">
                {{ t('admin.documentDetail.backToFileSelection') }}
              </button>
              <div class="flex gap-3">
                <button type="button" @click="closeImportCSVModal" class="bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors">{{ t('common.cancel') }}</button>
                <button @click="confirmImportCSV" :disabled="importingCSV || signersToImport.length === 0" class="trust-gradient text-white font-medium rounded-lg px-4 py-2.5 text-sm hover:opacity-90 transition-opacity disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center gap-2">
                  <Loader2 v-if="importingCSV" :size="16" class="animate-spin" />
                  {{ importingCSV ? t('admin.documentDetail.importing') : t('admin.documentDetail.importButton', { count: signersToImport.length }) }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

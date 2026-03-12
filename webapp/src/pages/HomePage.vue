<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useSignatureStore } from '@/stores/signatures'
import { useI18n } from 'vue-i18n'
import { usePageTitle } from '@/composables/usePageTitle'

const { t, locale } = useI18n()
usePageTitle('sign.title')

import {
  AlertTriangle,
  CheckCircle2,
  FileText,
  Users,
  Loader2,
  Shield,
  Zap,
  Clock,
  ExternalLink,
  Download,
  Check,
  Eye,
  Sparkles,
  Lock
} from 'lucide-vue-next'
import SignButton from '@/components/SignButton.vue'
import SignatureList from '@/components/SignatureList.vue'
import DocumentViewer from '@/components/viewer/DocumentViewer.vue'
import DocumentCreateForm from '@/components/DocumentCreateForm.vue'
import { documentService, type FindOrCreateDocumentResponse } from '@/services/documents'
import { detectReference } from '@/services/referenceDetector'
import { calculateFileChecksum } from '@/services/checksumCalculator'
import { updateDocumentMetadata } from '@/services/admin'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const signatureStore = useSignatureStore()

const docId = ref<string | undefined>(undefined)
const user = computed(() => authStore.user)
const isAuthenticated = computed(() => authStore.isAuthenticated)
const canCreateDocuments = computed(() => authStore.canCreateDocuments)
const currentDocument = ref<FindOrCreateDocumentResponse | null>(null)


const documentSignatures = ref<any[]>([])
const loadingSignatures = ref(false)
const loadingDocument = ref(false)
const showSuccessMessage = ref(false)
const errorMessage = ref<string | null>(null)
const needsAuth = ref(false)
const calculatingChecksum = ref(false)

// New state for integrated viewer
const readComplete = ref(false)
const certifyChecked = ref(false)
const documentLoadFailed = ref(false)

// Check if current user has signed this document
const userHasSigned = computed(() => {
  if (!user.value?.email || documentSignatures.value.length === 0) {
    return false
  }
  return documentSignatures.value.some(sig => sig.userEmail === user.value?.email)
})

// Get user's signature if exists
const userSignature = computed(() => {
  if (!user.value?.email || documentSignatures.value.length === 0) {
    return null
  }
  return documentSignatures.value.find(sig => sig.userEmail === user.value?.email)
})

// Document properties
const isIntegratedMode = computed(() => currentDocument.value?.readMode === 'integrated')
const requiresFullRead = computed(() => currentDocument.value?.requireFullRead ?? false)
const allowDownload = computed(() => currentDocument.value?.allowDownload ?? false)

// Can confirm: checkbox checked AND (if requireFullRead: must have completed read, unless viewer failed)
const canConfirm = computed(() => {
  if (!certifyChecked.value) return false
  if (isIntegratedMode.value && requiresFullRead.value && !readComplete.value && !documentLoadFailed.value) return false
  return true
})

async function loadDocumentSignatures() {
  if (!docId.value) return

  loadingSignatures.value = true
  try {
    documentSignatures.value = await signatureStore.fetchDocumentSignatures(docId.value)

    // If user has already signed, mark read as complete
    if (userHasSigned.value) {
      readComplete.value = true
    }
  } catch (error) {
    console.error('Failed to load document signatures:', error)
  } finally {
    loadingSignatures.value = false
  }
}

async function handleDocumentReference(ref: string) {
  try {
    loadingDocument.value = true
    errorMessage.value = null
    needsAuth.value = false
    readComplete.value = false
    certifyChecked.value = false
    documentLoadFailed.value = false

    console.log('Loading document for reference:', ref)

    // Detect reference type
    const refInfo = detectReference(ref)
    console.log('Reference detected as:', refInfo)

    // Call find-or-create API
    const doc = await documentService.findOrCreateDocument(ref)
    console.log('Document loaded:', doc)

    docId.value = doc.docId
    currentDocument.value = doc

    // If the ref is not the same as the docID, redirect to clean URL
    if (ref !== doc.docId) {
      await router.replace({
        name: route.name as string,
        query: { doc: doc.docId }
      })
    }

    // If new document AND downloadable URL → calculate checksum
    if (doc.isNew && refInfo.isDownloadable && refInfo.type === 'url' && !doc.checksum) {
      await calculateAndUpdateChecksum(doc.docId, refInfo.value)
    }

    // Load signatures
    await loadDocumentSignatures()
  } catch (error: any) {
    console.error('Failed to load/create document:', error)

    if (error.response?.status === 401) {
      errorMessage.value = t('sign.error.authRequired')
      needsAuth.value = true
    } else {
      errorMessage.value = error.message || t('sign.error.loadFailed')
      needsAuth.value = false
    }
  } finally {
    loadingDocument.value = false
  }
}

function handleLoginClick() {
  authStore.startOAuthLogin(route.fullPath)
}


async function calculateAndUpdateChecksum(docId: string, url: string) {
  try {
    calculatingChecksum.value = true
    console.log('Calculating checksum for:', url)

    const checksumData = await calculateFileChecksum(url)
    console.log('Checksum calculated:', checksumData.checksum)

    if (authStore.isAdmin) {
      await updateDocumentMetadata(docId, {
        checksum: checksumData.checksum,
        checksumAlgorithm: checksumData.algorithm
      })

      if (currentDocument.value) {
        currentDocument.value.checksum = checksumData.checksum
        currentDocument.value.checksumAlgorithm = checksumData.algorithm
      }

      console.log('Checksum updated in database')
    } else {
      console.log('Checksum calculated but not saved (user not admin)')
    }
  } catch (error) {
    console.warn('Checksum calculation failed:', error)
  } finally {
    calculatingChecksum.value = false
  }
}

function handleReadComplete() {
  readComplete.value = true
}

function handleDocumentLoadError(error: string) {
  console.log('Document load failed:', error)
  documentLoadFailed.value = true
}

async function handleSigned() {
  showSuccessMessage.value = true
  errorMessage.value = null
  certifyChecked.value = false

  await loadDocumentSignatures()

  // Update signature count to reflect the new signature
  if (currentDocument.value) {
    currentDocument.value.signatureCount = documentSignatures.value.length
  }

  setTimeout(() => {
    showSuccessMessage.value = false
  }, 5000)
}

function handleError(error: string) {
  errorMessage.value = error
  showSuccessMessage.value = false
}

function formatDate(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString(locale.value, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

function downloadProof() {
  if (!userSignature.value || !currentDocument.value) return

  const proof = {
    document: {
      id: currentDocument.value.docId,
      title: currentDocument.value.title,
      url: currentDocument.value.url,
      checksum: currentDocument.value.checksum,
      algorithm: currentDocument.value.checksumAlgorithm
    },
    signature: {
      email: userSignature.value.userEmail,
      name: userSignature.value.userName,
      signedAt: userSignature.value.signedAt,
      signature: userSignature.value.signature,
      payloadHash: userSignature.value.payloadHash,
      nonce: userSignature.value.nonce
    },
    generatedAt: new Date().toISOString()
  }

  const blob = new Blob([JSON.stringify(proof, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `proof-${currentDocument.value.docId}-${Date.now()}.json`
  a.click()
  URL.revokeObjectURL(url)
}

// Helper to wait for auth to be initialized by App.vue
async function waitForAuth() {
  if (authStore.initialized) return

  return new Promise<void>((resolve) => {
    const stopWatch = watch(
      () => authStore.initialized,
      (isInit) => {
        if (isInit) {
          stopWatch()
          resolve()
        }
      },
      { immediate: true }
    )
  })
}

// Watch for route query changes
watch(() => route.query.doc, async (newRef, oldRef) => {
  if (newRef === oldRef) return

  showSuccessMessage.value = false
  errorMessage.value = null
  needsAuth.value = false
  docId.value = undefined
  currentDocument.value = null
  documentSignatures.value = []
  readComplete.value = false
  certifyChecked.value = false

  if (newRef && typeof newRef === 'string') {
    await waitForAuth()
    await handleDocumentReference(newRef)
  }
})

onMounted(async () => {
  await waitForAuth()

  const ref = route.query.doc as string | undefined
  if (ref) {
    await handleDocumentReference(ref)
  }
})
</script>

<template>
  <div class="min-h-[calc(100vh-8rem)]">
    <div class="mx-auto max-w-7xl px-4 sm:px-6 py-6 sm:py-8">

      <!-- Error Message -->
      <transition
        enter-active-class="transition ease-out duration-300"
        enter-from-class="opacity-0 translate-y-2"
        enter-to-class="opacity-100 translate-y-0"
        leave-active-class="transition ease-in duration-200"
        leave-from-class="opacity-100 translate-y-0"
        leave-to-class="opacity-0 translate-y-2"
      >
        <div v-if="errorMessage && !loadingDocument" class="mb-6 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
          <div class="flex items-start">
            <AlertTriangle :size="20" class="mr-3 mt-0.5 text-red-600 dark:text-red-400 flex-shrink-0" />
            <div class="flex-1">
              <h3 class="font-medium text-red-900 dark:text-red-200">{{ t('sign.error.title') }}</h3>
              <p class="mt-1 text-sm text-red-700 dark:text-red-300">{{ errorMessage }}</p>
              <div v-if="needsAuth" class="mt-4">
                <button
                  @click="handleLoginClick"
                  class="trust-gradient text-white font-medium rounded-lg px-4 py-2.5 text-sm hover:opacity-90 transition-opacity"
                >
                  {{ t('sign.error.loginButton') }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </transition>

      <!-- Loading state -->
      <div v-if="loadingDocument" class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-12 text-center">
        <Loader2 :size="48" class="mx-auto mb-4 animate-spin text-blue-600" />
        <h2 class="text-xl font-semibold text-slate-900 dark:text-slate-100 mb-2">{{ t('sign.loading.title') }}</h2>
        <p class="text-slate-500 dark:text-slate-400">{{ t('sign.loading.description') }}</p>
      </div>

      <!-- No Document: Hero Section -->
      <div v-else-if="!docId" class="space-y-16">
        <!-- Hero -->
        <div class="text-center pt-8 sm:pt-12">
          <h1 class="mb-4 text-3xl sm:text-4xl lg:text-5xl font-bold tracking-tight text-slate-900 dark:text-slate-50">
            {{ t('home.hero.title') }}
          </h1>
          <p class="text-lg sm:text-xl text-slate-500 dark:text-slate-400 max-w-2xl mx-auto mb-8">
            {{ t('home.hero.subtitle') }}
          </p>

          <!-- Quick Create Card -->
          <div class="max-w-xl mx-auto">
            <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6 shadow-sm">
              <!-- Creation restricted message -->
              <div v-if="isAuthenticated && !canCreateDocuments" class="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-4">
                <div class="flex items-start gap-3">
                  <Lock :size="20" class="mt-0.5 text-amber-600 dark:text-amber-400 flex-shrink-0" />
                  <div class="text-left">
                    <p class="font-medium text-amber-900 dark:text-amber-200">{{ t('home.hero.restricted.title') }}</p>
                    <p class="text-sm text-amber-700 dark:text-amber-300 mt-1">{{ t('home.hero.restricted.description') }}</p>
                  </div>
                </div>
              </div>

              <!-- Document Create Form -->
              <DocumentCreateForm
                v-else
                mode="hero"
                :show-upload-button="isAuthenticated"
                redirect-route="document-edit"
              />

              <p class="mt-4 text-xs text-slate-500 dark:text-slate-400">
                {{ t('home.hero.form.hint') }}
              </p>
            </div>
          </div>
        </div>

        <!-- How it Works Section -->
        <div class="border-t border-slate-200 dark:border-slate-700 pt-16">
          <div class="text-center mb-12">
            <h2 class="mb-3 text-2xl sm:text-3xl font-bold tracking-tight text-slate-900 dark:text-slate-100">
              {{ t('home.howItWorks.title') }}
            </h2>
            <p class="text-slate-500 dark:text-slate-400 max-w-2xl mx-auto">
              {{ t('home.howItWorks.subtitle') }}
            </p>
          </div>

          <!-- Steps Grid -->
          <div class="grid gap-8 grid-cols-1 md:grid-cols-3 mb-12">
            <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6 text-center hover:shadow-md transition-shadow">
              <div class="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-xl bg-blue-50 dark:bg-blue-900/30">
                <FileText :size="28" class="text-blue-600 dark:text-blue-400" />
              </div>
              <h3 class="mb-2 text-lg font-semibold text-slate-900 dark:text-slate-100">{{ t('home.howItWorks.step1.title') }}</h3>
              <p class="text-sm text-slate-500 dark:text-slate-400">
                {{ t('home.howItWorks.step1.description') }}
              </p>
            </div>

            <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6 text-center hover:shadow-md transition-shadow">
              <div class="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-xl bg-blue-50 dark:bg-blue-900/30">
                <Eye :size="28" class="text-blue-600 dark:text-blue-400" />
              </div>
              <h3 class="mb-2 text-lg font-semibold text-slate-900 dark:text-slate-100">{{ t('home.howItWorks.step2.title') }}</h3>
              <p class="text-sm text-slate-500 dark:text-slate-400">
                {{ t('home.howItWorks.step2.description') }}
              </p>
            </div>

            <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6 text-center hover:shadow-md transition-shadow">
              <div class="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-xl bg-emerald-50 dark:bg-emerald-900/30">
                <Shield :size="28" class="text-emerald-600 dark:text-emerald-400" />
              </div>
              <h3 class="mb-2 text-lg font-semibold text-slate-900 dark:text-slate-100">{{ t('home.howItWorks.step3.title') }}</h3>
              <p class="text-sm text-slate-500 dark:text-slate-400">
                {{ t('home.howItWorks.step3.description') }}
              </p>
            </div>
          </div>

          <!-- Features -->
          <div class="grid gap-6 grid-cols-1 md:grid-cols-3">
            <div class="flex items-start space-x-3">
              <div class="rounded-lg bg-blue-50 dark:bg-blue-900/30 p-2 mt-1 flex-shrink-0">
                <Shield :size="20" class="text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <h4 class="font-medium text-slate-900 dark:text-slate-100 mb-1">{{ t('home.features.crypto.title') }}</h4>
                <p class="text-sm text-slate-500 dark:text-slate-400">
                  {{ t('home.features.crypto.description') }}
                </p>
              </div>
            </div>

            <div class="flex items-start space-x-3">
              <div class="rounded-lg bg-blue-50 dark:bg-blue-900/30 p-2 mt-1 flex-shrink-0">
                <Zap :size="20" class="text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <h4 class="font-medium text-slate-900 dark:text-slate-100 mb-1">{{ t('home.features.instant.title') }}</h4>
                <p class="text-sm text-slate-500 dark:text-slate-400">
                  {{ t('home.features.instant.description') }}
                </p>
              </div>
            </div>

            <div class="flex items-start space-x-3">
              <div class="rounded-lg bg-blue-50 dark:bg-blue-900/30 p-2 mt-1 flex-shrink-0">
                <Clock :size="20" class="text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <h4 class="font-medium text-slate-900 dark:text-slate-100 mb-1">{{ t('home.features.timestamp.title') }}</h4>
                <p class="text-sm text-slate-500 dark:text-slate-400">
                  {{ t('home.features.timestamp.description') }}
                </p>
              </div>
            </div>
          </div>
        </div>

        <!-- SaaS CTA Section -->
        <div class="border-t border-slate-200 dark:border-slate-700 pt-16">
          <div class="bg-gradient-to-br from-slate-50 to-blue-50 dark:from-slate-800 dark:to-blue-900/20 rounded-2xl border border-slate-200 dark:border-slate-700 p-8 sm:p-12 text-center">
            <!-- Coming Soon Badge -->
            <div class="inline-flex items-center gap-2 bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300 text-xs font-medium px-3 py-1 rounded-full mb-6">
              <Sparkles :size="14" />
              {{ t('home.saas.badge') }}
            </div>

            <h2 class="text-2xl sm:text-3xl font-bold text-slate-900 dark:text-slate-100 mb-4">
              {{ t('home.saas.title') }}
            </h2>
            <p class="text-slate-600 dark:text-slate-400 max-w-xl mx-auto mb-8">
              {{ t('home.saas.description') }}
            </p>

            <button
              disabled
              class="inline-flex items-center gap-2 bg-slate-300 dark:bg-slate-600 text-slate-500 dark:text-slate-400 font-medium rounded-lg px-6 py-3 text-sm cursor-not-allowed"
            >
              {{ t('home.saas.button') }}
            </button>

            <p class="mt-4 text-xs text-slate-500 dark:text-slate-500">
              {{ t('home.saas.note') }}
            </p>
          </div>
        </div>
      </div>

      <!-- Main Content when doc ID is present -->
      <div v-else-if="docId && currentDocument" class="space-y-6">
        <!-- Success Message -->
        <transition
          enter-active-class="transition ease-out duration-300"
          enter-from-class="opacity-0 translate-y-2"
          enter-to-class="opacity-100 translate-y-0"
          leave-active-class="transition ease-in duration-200"
          leave-from-class="opacity-100 translate-y-0"
          leave-to-class="opacity-0 translate-y-2"
        >
          <div v-if="showSuccessMessage" class="bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-xl p-4">
            <div class="flex items-start">
              <CheckCircle2 :size="20" class="mr-3 mt-0.5 text-emerald-600 dark:text-emerald-400 flex-shrink-0" />
              <div class="flex-1">
                <h3 class="font-medium text-emerald-900 dark:text-emerald-200">{{ t('sign.success.title') }}</h3>
                <p class="mt-1 text-sm text-emerald-700 dark:text-emerald-300">{{ t('sign.success.description') }}</p>
              </div>
            </div>
          </div>
        </transition>

        <!-- Document Header -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-4 sm:p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center flex-shrink-0">
              <FileText :size="24" class="text-blue-600 dark:text-blue-400" />
            </div>
            <div class="flex-1 min-w-0">
              <h1 class="text-lg sm:text-xl font-semibold text-slate-900 dark:text-slate-100">
                {{ currentDocument.title || t('sign.document.title') }}
              </h1>
              <p v-if="currentDocument.url" class="mt-1 text-sm text-slate-500 dark:text-slate-400 font-mono text-xs break-all">
                {{ currentDocument.url }}
              </p>
              <p v-else class="mt-1 text-sm text-slate-500 dark:text-slate-400">
                <span class="font-mono text-xs">{{ t('sign.document.id') }}: {{ docId }}</span>
              </p>
            </div>
          </div>
        </div>

        <!-- Two Column Layout -->
        <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <!-- Left: Document Zone (2/3) -->
          <div class="lg:col-span-2 space-y-6">
            <!-- Integrated Viewer -->
            <div v-if="isIntegratedMode && (currentDocument.url || currentDocument.storageKey) && !documentLoadFailed">
              <DocumentViewer
                :document-id="docId"
                :url="currentDocument.url || ''"
                :allow-download="allowDownload"
                :require-full-read="requiresFullRead"
                :is-stored="!!currentDocument.storageKey"
                :stored-mime-type="currentDocument.mimeType"
                :already-read="userHasSigned"
                :verify-checksum="currentDocument.verifyChecksum"
                :stored-checksum="currentDocument.checksum"
                :checksum-algorithm="currentDocument.checksumAlgorithm"
                @read-complete="handleReadComplete"
                @load-error="handleDocumentLoadError"
              />
            </div>

            <!-- External Mode / Load Failed -->
            <div v-else class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-8 text-center">
              <div class="w-16 h-16 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center mx-auto mb-4">
                <ExternalLink :size="32" class="text-blue-600 dark:text-blue-400" />
              </div>

              <!-- Integrated mode viewer failed — likely local network / inaccessible resource -->
              <template v-if="documentLoadFailed && isIntegratedMode">
                <h3 class="text-lg font-semibold text-slate-900 dark:text-slate-100 mb-2">
                  {{ t('sign.external.localNetworkTitle') }}
                </h3>
                <p class="text-sm text-slate-500 dark:text-slate-400 mb-4 max-w-md mx-auto">
                  {{ t('sign.external.localNetworkDescription') }}
                </p>
              </template>

              <!-- Normal external mode -->
              <template v-else>
                <h3 class="text-lg font-semibold text-slate-900 dark:text-slate-100 mb-2">
                  {{ t('sign.external.title') }}
                </h3>
                <p class="text-sm text-slate-500 dark:text-slate-400 mb-4 max-w-md mx-auto">
                  {{ currentDocument.url ? t('sign.external.descriptionWithUrl') : t('sign.external.description') }}
                </p>
              </template>

              <!-- Document description if available -->
              <p v-if="currentDocument.description" class="text-sm text-slate-600 dark:text-slate-300 mb-6 max-w-md mx-auto italic">
                {{ currentDocument.description }}
              </p>

              <div v-if="currentDocument.url" class="space-y-4">
                <div class="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4 max-w-lg mx-auto">
                  <p class="text-xs text-slate-500 dark:text-slate-400 mb-1">{{ t('sign.external.documentUrl') }}</p>
                  <a
                    :href="currentDocument.url"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="text-sm text-blue-600 dark:text-blue-400 hover:underline break-all"
                  >
                    {{ currentDocument.url }}
                  </a>
                </div>
                <a
                  :href="currentDocument.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="inline-flex items-center gap-2 trust-gradient text-white font-medium rounded-lg px-6 py-3 text-sm hover:opacity-90 transition-opacity"
                >
                  <ExternalLink :size="18" />
                  {{ t('sign.external.openDocument') }}
                </a>
              </div>
              <p v-else class="text-sm text-slate-400 dark:text-slate-500 italic">
                {{ t('sign.external.noUrl') }}
              </p>
            </div>

            <!-- Existing Confirmations (Below document on mobile, here on desktop) -->
            <!-- Show section if there are signatures (count from document, even if user can't see details) -->
            <div v-if="currentDocument.signatureCount > 0" class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700">
              <div class="p-4 sm:p-6" :class="{ 'border-b border-slate-100 dark:border-slate-700': documentSignatures.length > 0 }">
                <div class="flex items-center gap-3">
                  <div class="w-10 h-10 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center">
                    <Users :size="20" class="text-blue-600 dark:text-blue-400" />
                  </div>
                  <div>
                    <h3 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('sign.confirmations.title') }}</h3>
                    <p class="text-sm text-slate-500 dark:text-slate-400">
                      {{ t('sign.confirmations.count', { count: currentDocument.signatureCount }, currentDocument.signatureCount) }}
                    </p>
                  </div>
                </div>
              </div>
              <!-- Only show detailed list if user has access (owner/admin) -->
              <div v-if="documentSignatures.length > 0" class="p-4 sm:p-6">
                <SignatureList
                  :signatures="documentSignatures"
                  :loading="loadingSignatures"
                  :show-details="true"
                  :compact="true"
                />
              </div>
            </div>
          </div>

          <!-- Right: Confirmation Panel (1/3) -->
          <div class="lg:col-span-1">
            <div class="lg:sticky lg:top-24 space-y-4">
              <!-- Already Signed Panel -->
              <div v-if="userHasSigned && userSignature" class="bg-emerald-50 dark:bg-emerald-900/20 rounded-xl border border-emerald-200 dark:border-emerald-800 p-6">
                <div class="flex items-center gap-3 mb-4">
                  <div class="w-10 h-10 rounded-full bg-emerald-100 dark:bg-emerald-900/50 flex items-center justify-center">
                    <Check :size="20" class="text-emerald-600 dark:text-emerald-400" />
                  </div>
                  <div>
                    <h3 class="font-semibold text-emerald-900 dark:text-emerald-200">{{ t('sign.alreadySigned.title') }}</h3>
                    <p class="text-xs text-emerald-700 dark:text-emerald-400">{{ formatDate(userSignature.signedAt) }}</p>
                  </div>
                </div>

                <div class="space-y-3 text-sm">
                  <div class="flex justify-between">
                    <span class="text-emerald-700 dark:text-emerald-400">{{ t('sign.alreadySigned.signedBy') }}</span>
                    <span class="font-medium text-emerald-900 dark:text-emerald-200">{{ userSignature.userName || userSignature.userEmail }}</span>
                  </div>
                  <div class="flex justify-between">
                    <span class="text-emerald-700 dark:text-emerald-400">{{ t('sign.alreadySigned.email') }}</span>
                    <span class="font-mono text-xs text-emerald-900 dark:text-emerald-200">{{ userSignature.userEmail }}</span>
                  </div>
                  <div class="flex justify-between">
                    <span class="text-emerald-700 dark:text-emerald-400">{{ t('sign.alreadySigned.signatureType') }}</span>
                    <span class="font-mono text-xs text-emerald-900 dark:text-emerald-200">Ed25519</span>
                  </div>
                </div>

                <button
                  @click="downloadProof"
                  class="mt-4 w-full inline-flex items-center justify-center gap-2 bg-emerald-600 hover:bg-emerald-700 text-white font-medium rounded-lg px-4 py-2.5 text-sm transition-colors"
                >
                  <Download :size="16" />
                  {{ t('sign.alreadySigned.downloadProof') }}
                </button>
              </div>

              <!-- Not Yet Signed Panel -->
              <div v-else class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6">
                <h3 class="font-semibold text-slate-900 dark:text-slate-100 mb-4">{{ t('sign.confirm.title') }}</h3>

                <!-- Warning if requireFullRead and not completed (hidden if viewer failed — user can't read in viewer) -->
                <div v-if="isIntegratedMode && requiresFullRead && !readComplete && !documentLoadFailed" class="mb-4 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3">
                  <div class="flex items-start gap-2">
                    <AlertTriangle :size="16" class="mt-0.5 text-amber-600 dark:text-amber-400 flex-shrink-0" />
                    <p class="text-sm text-amber-800 dark:text-amber-200">{{ t('sign.confirm.readRequired') }}</p>
                  </div>
                </div>

                <!-- Info box -->
                <div class="accent-border bg-blue-50 dark:bg-blue-900/20 rounded-r-lg p-3 mb-4">
                  <p class="text-xs text-blue-800 dark:text-blue-200">{{ t('sign.info.description') }}</p>
                </div>

                <!-- What will be recorded -->
                <div class="mb-4">
                  <p class="text-xs font-medium text-slate-600 dark:text-slate-400 mb-2">{{ t('sign.info.recorded') }}</p>
                  <ul class="text-xs text-slate-500 dark:text-slate-400 space-y-1">
                    <li class="flex items-center gap-2">
                      <div class="w-1 h-1 rounded-full bg-blue-500"></div>
                      {{ t('sign.info.email') }}: <span class="font-medium text-slate-700 dark:text-slate-300">{{ user?.email }}</span>
                    </li>
                    <li class="flex items-center gap-2">
                      <div class="w-1 h-1 rounded-full bg-blue-500"></div>
                      {{ t('sign.info.timestamp') }}
                    </li>
                    <li class="flex items-center gap-2">
                      <div class="w-1 h-1 rounded-full bg-blue-500"></div>
                      {{ t('sign.info.signature') }}
                    </li>
                    <li class="flex items-center gap-2">
                      <div class="w-1 h-1 rounded-full bg-blue-500"></div>
                      {{ t('sign.info.hash') }}
                    </li>
                  </ul>
                </div>

                <!-- Checkbox -->
                <label class="flex items-start gap-3 mb-4 cursor-pointer">
                  <input
                    type="checkbox"
                    v-model="certifyChecked"
                    class="mt-0.5 rounded border-slate-300 dark:border-slate-600 text-blue-600 focus:ring-blue-500"
                  />
                  <span class="text-sm text-slate-700 dark:text-slate-300">
                    {{ t('sign.confirm.certify') }}
                  </span>
                </label>

                <!-- Sign Button -->
                <SignButton
                  :doc-id="docId"
                  :signatures="documentSignatures"
                  :disabled="!canConfirm"
                  @signed="handleSigned"
                  @error="handleError"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

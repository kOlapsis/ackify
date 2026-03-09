<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<template>
  <div class="min-h-screen bg-slate-50 dark:bg-slate-900 p-4 font-sans">
    <!-- Loading state -->
    <div v-if="loading" class="flex items-center justify-center py-12">
      <svg class="animate-spin h-8 w-8 text-blue-600 dark:text-blue-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
      </svg>
    </div>

    <!-- Error state -->
    <div v-else-if="error" class="max-w-md mx-auto">
      <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
        <div class="flex items-start gap-3">
          <div class="w-8 h-8 rounded-lg bg-red-100 dark:bg-red-900/30 flex items-center justify-center flex-shrink-0">
            <svg class="w-4 h-4 text-red-600 dark:text-red-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
          </div>
          <p class="text-red-700 dark:text-red-400 text-sm">{{ error }}</p>
        </div>
      </div>
    </div>

    <!-- Document info and signatures -->
    <div v-else-if="documentData" class="max-w-2xl mx-auto">
      <!-- Document header with signatures (shown if there are confirmations) -->
      <div v-if="signatureCount > 0">
        <!-- Header Card -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-4 sm:p-5 mb-4">
          <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
            <div class="min-w-0">
              <h2 class="text-lg sm:text-xl font-bold text-slate-900 dark:text-white truncate">
                {{ documentData.title }}
              </h2>
              <div class="flex items-center gap-2 mt-2 text-sm text-slate-500 dark:text-slate-400">
                <span class="inline-flex items-center gap-1.5 px-2.5 py-1 bg-emerald-50 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-400 text-xs font-medium rounded-full">
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7"/>
                  </svg>
                  {{ t('embed.confirmationsCount', { count: signatureCount }) }}
                </span>
              </div>
            </div>
            <!-- Sign button -->
            <a
              :href="signUrl"
              target="_blank"
              class="inline-flex items-center justify-center gap-2 trust-gradient text-white font-medium rounded-lg px-5 py-2.5 hover:opacity-90 transition-opacity whitespace-nowrap min-h-[44px]"
            >
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
              </svg>
              {{ t('embed.sign') }}
            </a>
          </div>
        </div>

        <!-- Signatures list (only shown if user has access to view signatures) -->
        <div v-if="documentData.signatures && documentData.signatures.length > 0" class="space-y-2" data-testid="signatures-list">
          <div
            v-for="signature in documentData.signatures"
            :key="signature.id"
            data-testid="signature-item"
            class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 px-4 py-3 flex items-center justify-between gap-3"
          >
            <div class="flex items-center gap-3 min-w-0 flex-1">
              <div class="w-8 h-8 rounded-full verified-gradient flex items-center justify-center flex-shrink-0">
                <svg class="w-4 h-4 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7"/>
                </svg>
              </div>
              <span class="text-sm font-medium text-slate-900 dark:text-white truncate">{{ signature.userEmail }}</span>
            </div>
            <span data-testid="signature-date" class="text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{{ formatDateCompact(signature.signedAt) }}</span>
          </div>
        </div>
      </div>

      <!-- Empty state - No signatures yet -->
      <div v-else class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-8 sm:p-12 text-center">
        <div class="w-16 h-16 mx-auto bg-slate-100 dark:bg-slate-700 rounded-2xl flex items-center justify-center mb-4">
          <svg class="w-8 h-8 text-slate-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"/>
          </svg>
        </div>
        <p class="text-slate-500 dark:text-slate-400 mb-6">{{ t('embed.noSignatures') }}</p>
        <a
          :href="signUrl"
          target="_blank"
          class="inline-flex items-center justify-center gap-2 trust-gradient text-white font-medium rounded-lg px-6 py-3 hover:opacity-90 transition-opacity min-h-[48px]"
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/>
          </svg>
          {{ t('embed.signDocument') }}
        </a>
      </div>

      <!-- Footer branding -->
      <div class="mt-6 pt-4 border-t border-slate-200 dark:border-slate-700 text-center">
        <a
          href="https://github.com/kolapsis/ackify"
          target="_blank"
          class="inline-flex items-center gap-1.5 text-xs text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-400 transition-colors"
        >
          <svg class="w-3.5 h-3.5" fill="currentColor" viewBox="0 0 24 24">
            <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
          </svg>
          {{ t('embed.poweredBy') }}
        </a>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePageTitle } from '@/composables/usePageTitle'
import { documentService } from '@/services/documents'
import http, { extractError } from '@/services/http'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
usePageTitle('embed.title')

// State
const loading = ref(false)
const error = ref<string | null>(null)
const documentData = ref<any>(null)
const resolvedDocId = ref<string | null>(null)
const signatureCount = ref<number>(0)

// Computed
const docRef = computed(() => route.query.doc as string)

const signUrl = computed(() => {
  const baseUrl = (window as any).ACKIFY_BASE_URL || window.location.origin
  return `${baseUrl}/?doc=${encodeURIComponent(docRef.value)}`
})

// Methods
function formatDateCompact(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString('fr-FR', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric'
  })
}

async function loadDocument() {
  if (!docRef.value) {
    error.value = t('embed.missingDocId')
    loading.value = false
    return
  }

  try {
    loading.value = true
    error.value = null

    // First, find or create the document to get the docID
    const doc = await documentService.findOrCreateDocument(docRef.value)
    resolvedDocId.value = doc.docId
    signatureCount.value = doc.signatureCount || 0

    // If the docRef is not the same as the docID, redirect to clean URL
    if (docRef.value !== doc.docId) {
      await router.replace({
        name: route.name as string,
        query: { doc: doc.docId }
      })
      return // Router will trigger watch and reload
    }

    // Then fetch signatures using the resolved docID (may return empty list for non-owner)
    const response = await http.get(`/documents/${doc.docId}/signatures`)

    // Build document data from signatures response
    const signatures = response.data.data || []
    documentData.value = {
      id: doc.docId,
      title: doc.title || `Document ${doc.docId}`,
      signatures: signatures,
      metadata: {}
    }

  } catch (err: any) {
    error.value = extractError(err)
  } finally {
    loading.value = false
  }
}

// Watch for changes in doc query param (for navigation)
watch(() => route.query.doc, () => {
  if (route.query.doc) {
    loadDocument()
  }
})

onMounted(() => {
  loadDocument()
})
</script>

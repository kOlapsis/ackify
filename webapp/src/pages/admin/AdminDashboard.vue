<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePageTitle } from '@/composables/usePageTitle'
import { listDocuments, deleteDocument, type Document } from '@/services/admin'
import { extractError } from '@/services/http'
import DocumentCreateForm from '@/components/DocumentCreateForm.vue'
import type { FindOrCreateDocumentResponse } from '@/services/documents'
import {
  FileText,
  Clock,
  CheckCircle,
  Eye,
  Copy,
  Trash2,
  Loader2,
  Search,
  Webhook,
  Settings,
  ChevronLeft,
  ChevronRight,
  AlertCircle,
  RefreshCw,
  Check,
} from 'lucide-vue-next'

const router = useRouter()
const { t, locale } = useI18n()
usePageTitle('admin.title')
const documents = ref<Document[]>([])
const loading = ref(true)
const searching = ref(false)
const error = ref('')

// Pagination & Search
const searchQuery = ref('')
const currentPage = ref(1)
const perPage = ref(20)
const totalDocsCount = ref(0)
let searchTimeout: ReturnType<typeof setTimeout> | null = null

// Delete confirmation
const deletingDocId = ref<string | null>(null)
const showDeleteConfirm = ref(false)
const docToDelete = ref<Document | null>(null)

// Copy feedback
const copiedDocId = ref<string | null>(null)

// Computed
const totalPages = computed(() => Math.ceil(totalDocsCount.value / perPage.value) || 1)

// Stats
const totalDocuments = computed(() => totalDocsCount.value)
const pendingDocuments = ref(0)
const completedDocuments = ref(0)

// Base URL for share links
const baseUrl = computed(() => (window as any).ACKIFY_BASE_URL || window.location.origin)

async function loadDocuments(isInitialLoad = false) {
  try {
    if (isInitialLoad) {
      loading.value = true
    } else {
      searching.value = true
    }

    error.value = ''
    const offset = (currentPage.value - 1) * perPage.value

    const response = await listDocuments(
      perPage.value,
      offset,
      searchQuery.value || undefined
    )

    documents.value = response.data

    if (response.meta) {
      totalDocsCount.value = response.meta.total || documents.value.length
      pendingDocuments.value = response.meta.pendingDocuments ?? 0
      completedDocuments.value = response.meta.completedDocuments ?? 0
    } else {
      totalDocsCount.value = documents.value.length
    }
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to load documents:', err)
  } finally {
    loading.value = false
    searching.value = false
  }
}

function handleSearchInput() {
  if (searchTimeout) {
    clearTimeout(searchTimeout)
  }

  searchTimeout = setTimeout(() => {
    currentPage.value = 1
    loadDocuments()
  }, 300)
}

watch(searchQuery, () => {
  handleSearchInput()
})

function nextPage() {
  if (currentPage.value < totalPages.value) {
    currentPage.value++
    loadDocuments()
  }
}

function prevPage() {
  if (currentPage.value > 1) {
    currentPage.value--
    loadDocuments()
  }
}

function handleDocumentCreated(doc: FindOrCreateDocumentResponse) {
  // Navigate to admin document detail page
  router.push({ name: 'admin-document', params: { docId: doc.docId } })
}

function viewDocument(doc: Document) {
  router.push({ name: 'admin-document', params: { docId: doc.docId } })
}

async function copyShareLink(doc: Document) {
  const shareUrl = `${baseUrl.value}/?doc=${doc.docId}`
  try {
    await navigator.clipboard.writeText(shareUrl)
    copiedDocId.value = doc.docId
    setTimeout(() => {
      copiedDocId.value = null
    }, 2000)
  } catch (err) {
    console.error('Failed to copy link:', err)
  }
}

function confirmDelete(doc: Document) {
  docToDelete.value = doc
  showDeleteConfirm.value = true
}

async function handleDeleteDocument() {
  if (!docToDelete.value) return

  try {
    deletingDocId.value = docToDelete.value.docId
    await deleteDocument(docToDelete.value.docId)
    showDeleteConfirm.value = false
    docToDelete.value = null
    loadDocuments()
  } catch (err) {
    error.value = extractError(err)
    console.error('Failed to delete document:', err)
  } finally {
    deletingDocId.value = null
  }
}

function formatDate(dateString: string): string {
  const date = new Date(dateString)
  return date.toLocaleDateString(locale.value, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

onMounted(() => {
  loadDocuments(true)
})
</script>

<template>
  <div class="min-h-[calc(100vh-8rem)]">
    <main class="mx-auto max-w-6xl px-4 sm:px-6 py-6 sm:py-8">
      <!-- Page Header -->
      <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-8">
        <div>
          <h1 class="text-2xl sm:text-3xl font-bold tracking-tight text-slate-900 dark:text-slate-50">
            {{ t('admin.title') }}
          </h1>
          <p class="mt-1 text-base text-slate-500 dark:text-slate-400">
            {{ t('admin.subtitle') }}
          </p>
        </div>
        <div class="flex items-center gap-3">
          <router-link :to="{ name: 'admin-webhooks' }">
            <button class="inline-flex items-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">
              <Webhook :size="16" />
              <span class="hidden sm:inline">{{ t('admin.webhooks.manage') }}</span>
            </button>
          </router-link>
          <router-link :to="{ name: 'admin-settings' }">
            <button class="inline-flex items-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">
              <Settings :size="16" />
              <span class="hidden sm:inline">{{ t('admin.settings.manage') }}</span>
            </button>
          </router-link>
          <button
            @click="loadDocuments()"
            :disabled="loading || searching"
            class="inline-flex items-center justify-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-4 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors disabled:opacity-50"
          >
            <RefreshCw :size="16" :class="(loading || searching) ? 'animate-spin' : ''" />
            <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
          </button>
        </div>
      </div>

      <!-- Error Alert -->
      <div v-if="error" class="mb-6 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-xl p-4">
        <div class="flex items-start">
          <AlertCircle :size="20" class="mr-3 mt-0.5 text-red-600 dark:text-red-400 flex-shrink-0" />
          <div class="flex-1">
            <h3 class="font-medium text-red-900 dark:text-red-200">{{ t('common.error') }}</h3>
            <p class="mt-1 text-sm text-red-700 dark:text-red-300">{{ error }}</p>
          </div>
        </div>
      </div>

      <!-- Create Form -->
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-6 mb-8">
        <DocumentCreateForm
          mode="compact"
          :redirect-on-create="false"
          @created="handleDocumentCreated"
        />
      </div>

      <div v-if="loading" class="flex flex-col items-center justify-center py-24">
        <Loader2 :size="48" class="animate-spin text-blue-600" />
        <p class="mt-4 text-slate-500 dark:text-slate-400">{{ t('common.loading') }}</p>
      </div>

      <!-- Content -->
      <div v-else>
        <!-- Stats Pills Mobile -->
        <div class="md:hidden mb-6 grid grid-cols-3 gap-3">
          <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400">
            <FileText :size="18" />
            <span class="text-xl font-bold">{{ totalDocuments }}</span>
            <span class="text-xs whitespace-nowrap">{{ t('myDocuments.stats.total') }}</span>
          </div>
          <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400">
            <Clock :size="18" />
            <span class="text-xl font-bold">{{ pendingDocuments }}</span>
            <span class="text-xs whitespace-nowrap">{{ t('myDocuments.stats.pending') }}</span>
          </div>
          <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-emerald-50 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400">
            <CheckCircle :size="18" />
            <span class="text-xl font-bold">{{ completedDocuments }}</span>
            <span class="text-xs whitespace-nowrap">{{ t('myDocuments.stats.completed') }}</span>
          </div>
        </div>

        <!-- Stats Cards Desktop -->
        <div class="hidden md:grid mb-8 gap-6 sm:grid-cols-2 lg:grid-cols-3">
          <!-- Total Documents -->
          <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-5 hover:shadow-md transition-shadow">
            <div class="flex items-center gap-4">
              <div class="w-12 h-12 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center">
                <FileText :size="24" class="text-blue-600 dark:text-blue-400" />
              </div>
              <div>
                <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('myDocuments.stats.totalDocuments') }}</p>
                <p class="text-2xl font-bold text-slate-900 dark:text-slate-100">{{ totalDocuments }}</p>
              </div>
            </div>
          </div>

          <!-- Pending -->
          <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-5 hover:shadow-md transition-shadow">
            <div class="flex items-center gap-4">
              <div class="w-12 h-12 rounded-xl bg-amber-50 dark:bg-amber-900/30 flex items-center justify-center">
                <Clock :size="24" class="text-amber-600 dark:text-amber-400" />
              </div>
              <div>
                <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('myDocuments.stats.pendingConfirmations') }}</p>
                <p class="text-2xl font-bold text-slate-900 dark:text-slate-100">{{ pendingDocuments }}</p>
              </div>
            </div>
          </div>

          <!-- Completed -->
          <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-5 hover:shadow-md transition-shadow">
            <div class="flex items-center gap-4">
              <div class="w-12 h-12 rounded-xl bg-emerald-50 dark:bg-emerald-900/30 flex items-center justify-center">
                <CheckCircle :size="24" class="text-emerald-600 dark:text-emerald-400" />
              </div>
              <div>
                <p class="text-sm text-slate-500 dark:text-slate-400">{{ t('myDocuments.stats.completedDocuments') }}</p>
                <p class="text-2xl font-bold text-slate-900 dark:text-slate-100">{{ completedDocuments }}</p>
              </div>
            </div>
          </div>
        </div>

        <!-- Documents List -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700">
          <div class="p-6 border-b border-slate-100 dark:border-slate-700">
            <div class="flex flex-col gap-4">
              <div>
                <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('admin.documents.title') }}</h2>
                <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
                  {{ t('myDocuments.results', { count: totalDocsCount }) }}
                </p>
              </div>

              <div class="relative">
                <Search v-if="!searching" :size="18" class="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none" />
                <Loader2 v-else :size="18" class="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 animate-spin" />
                <input
                  v-model="searchQuery"
                  type="text"
                  :placeholder="t('myDocuments.searchPlaceholder')"
                  class="w-full pl-10 pr-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-500 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                />
              </div>
            </div>
          </div>

          <div class="p-6">
            <!-- Desktop Table -->
            <div v-if="documents.length > 0" class="hidden md:block overflow-x-auto">
              <table class="w-full">
                <thead>
                  <tr class="border-b border-slate-100 dark:border-slate-700">
                    <th class="px-4 py-3 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                      {{ t('myDocuments.columns.document') }}
                    </th>
                    <th class="px-4 py-3 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                      {{ t('admin.documents.by') }}
                    </th>
                    <th class="px-4 py-3 text-left text-xs font-medium text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                      {{ t('myDocuments.columns.createdAt') }}
                    </th>
                    <th class="px-4 py-3 text-right text-xs font-medium text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                      {{ t('myDocuments.columns.actions') }}
                    </th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-slate-100 dark:divide-slate-700">
                  <tr
                    v-for="doc in documents"
                    :key="doc.docId"
                    class="hover:bg-slate-50 dark:hover:bg-slate-700/50 transition-colors cursor-pointer"
                    @click="viewDocument(doc)"
                  >
                    <td class="px-4 py-4">
                      <div class="flex items-center gap-3">
                        <div class="w-10 h-10 rounded-lg bg-slate-100 dark:bg-slate-700 flex items-center justify-center flex-shrink-0">
                          <FileText :size="18" class="text-slate-500 dark:text-slate-400" />
                        </div>
                        <div class="min-w-0">
                          <div class="font-medium text-slate-900 dark:text-slate-100 truncate">{{ doc.title || doc.docId }}</div>
                          <div class="text-xs text-slate-500 dark:text-slate-400 font-mono truncate max-w-[250px]">
                            {{ doc.docId }}
                          </div>
                        </div>
                      </div>
                    </td>
                    <td class="px-4 py-4">
                      <span class="text-sm text-slate-500 dark:text-slate-400">{{ doc.createdBy }}</span>
                    </td>
                    <td class="px-4 py-4 text-sm text-slate-500 dark:text-slate-400">
                      {{ formatDate(doc.createdAt) }}
                    </td>
                    <td class="px-4 py-4 text-right" @click.stop>
                      <div class="flex items-center justify-end gap-1">
                        <button
                          @click="viewDocument(doc)"
                          class="p-2 text-slate-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg transition-colors"
                          :title="t('myDocuments.actions.view')"
                        >
                          <Eye :size="16" />
                        </button>
                        <button
                          @click="copyShareLink(doc)"
                          class="p-2 text-slate-400 hover:text-blue-600 dark:hover:text-blue-400 hover:bg-slate-100 dark:hover:bg-slate-700 rounded-lg transition-colors"
                          :title="t('myDocuments.actions.copyLink')"
                        >
                          <Check v-if="copiedDocId === doc.docId" :size="16" class="text-emerald-500" />
                          <Copy v-else :size="16" />
                        </button>
                        <button
                          @click="confirmDelete(doc)"
                          class="p-2 text-slate-400 hover:text-red-600 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 rounded-lg transition-colors"
                          :title="t('myDocuments.actions.delete')"
                        >
                          <Trash2 :size="16" />
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <!-- Mobile Cards -->
            <div v-if="documents.length > 0" class="md:hidden space-y-4">
              <div
                v-for="doc in documents"
                :key="doc.docId"
                class="bg-slate-50 dark:bg-slate-700/50 rounded-xl p-4"
                @click="viewDocument(doc)"
              >
                <!-- Document Title -->
                <div class="flex items-start gap-3 mb-3">
                  <div class="w-10 h-10 rounded-lg bg-white dark:bg-slate-800 flex items-center justify-center flex-shrink-0">
                    <FileText :size="18" class="text-slate-500 dark:text-slate-400" />
                  </div>
                  <div class="flex-1 min-w-0">
                    <h3 class="font-medium text-slate-900 dark:text-slate-100 truncate">{{ doc.title || doc.docId }}</h3>
                    <p class="text-xs text-slate-500 dark:text-slate-400 font-mono truncate">{{ doc.docId }}</p>
                  </div>
                </div>

                <!-- Meta Info -->
                <div class="flex items-center gap-3 text-sm text-slate-500 dark:text-slate-400 mb-3">
                  <span>{{ formatDate(doc.createdAt) }}</span>
                  <span class="text-xs">{{ doc.createdBy }}</span>
                </div>

                <!-- Actions -->
                <div class="flex gap-2 pt-3 border-t border-slate-200 dark:border-slate-600" @click.stop>
                  <button
                    @click="viewDocument(doc)"
                    class="flex-1 inline-flex items-center justify-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-3 py-2 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors"
                  >
                    <Eye :size="16" />
                    {{ t('myDocuments.actions.view') }}
                  </button>
                  <button
                    @click="copyShareLink(doc)"
                    class="inline-flex items-center justify-center gap-2 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-3 py-2 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors"
                  >
                    <Check v-if="copiedDocId === doc.docId" :size="16" class="text-emerald-500" />
                    <Copy v-else :size="16" />
                  </button>
                  <button
                    @click="confirmDelete(doc)"
                    class="inline-flex items-center justify-center bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-red-600 dark:text-red-400 font-medium rounded-lg px-3 py-2 text-sm hover:bg-red-50 dark:hover:bg-red-900/20 transition-colors"
                  >
                    <Trash2 :size="16" />
                  </button>
                </div>
              </div>
            </div>

            <!-- Empty State -->
            <div v-if="documents.length === 0" class="text-center py-12">
              <div class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-slate-100 dark:bg-slate-700">
                <FileText :size="28" class="text-slate-400" />
              </div>
              <h3 class="mb-2 text-lg font-semibold text-slate-900 dark:text-slate-100">
                {{ searchQuery ? t('myDocuments.noResults') : t('admin.documents.noDocuments') }}
              </h3>
              <p class="text-sm text-slate-500 dark:text-slate-400">
                {{ searchQuery ? t('myDocuments.tryAnotherSearch') : t('admin.documents.willAppear') }}
              </p>
            </div>

            <!-- Pagination -->
            <div v-if="documents.length > 0 && totalPages > 1" class="flex items-center justify-between mt-6 pt-4 border-t border-slate-200 dark:border-slate-700">
              <div class="text-sm text-slate-500 dark:text-slate-400 hidden md:block">
                {{ t('myDocuments.totalCount', totalDocsCount) }}
              </div>
              <div class="flex items-center gap-2 w-full md:w-auto justify-between md:justify-end">
                <button
                  :disabled="currentPage === 1"
                  @click="prevPage"
                  class="inline-flex items-center gap-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-3 py-2 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <ChevronLeft :size="16" />
                  {{ t('common.previous') }}
                </button>
                <span class="text-sm text-slate-500 dark:text-slate-400">
                  {{ t('myDocuments.pagination.page', { current: currentPage, total: totalPages }) }}
                </span>
                <button
                  :disabled="currentPage >= totalPages"
                  @click="nextPage"
                  class="inline-flex items-center gap-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-600 text-slate-700 dark:text-slate-200 font-medium rounded-lg px-3 py-2 text-sm hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {{ t('common.next') }}
                  <ChevronRight :size="16" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </main>

    <!-- Delete Confirmation Modal -->
    <Teleport to="body">
      <div
        v-if="showDeleteConfirm"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50"
        @click.self="showDeleteConfirm = false"
      >
        <div class="bg-white dark:bg-slate-800 rounded-xl shadow-xl max-w-md w-full p-6">
          <div class="flex items-start gap-4">
            <div class="w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center flex-shrink-0">
              <Trash2 :size="24" class="text-red-600 dark:text-red-400" />
            </div>
            <div class="flex-1">
              <h3 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
                {{ t('myDocuments.deleteConfirm.title') }}
              </h3>
              <p class="mt-2 text-sm text-slate-500 dark:text-slate-400">
                {{ t('myDocuments.deleteConfirm.message', { title: docToDelete?.title || docToDelete?.docId }) }}
              </p>
            </div>
          </div>
          <div class="flex justify-end gap-3 mt-6">
            <button
              @click="showDeleteConfirm = false"
              class="px-4 py-2 text-sm font-medium text-slate-700 dark:text-slate-200 bg-white dark:bg-slate-700 border border-slate-200 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-600 transition-colors"
            >
              {{ t('common.cancel') }}
            </button>
            <button
              @click="handleDeleteDocument"
              :disabled="deletingDocId !== null"
              class="px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-lg hover:bg-red-700 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              <Loader2 v-if="deletingDocId !== null" :size="16" class="animate-spin" />
              {{ t('common.delete') }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

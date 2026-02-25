<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSignatureStore } from '@/stores/signatures'
import { FileSignature, FileCheck, Clock, Search, Info, Loader2, AlertTriangle } from 'lucide-vue-next'
import SignatureList from '@/components/SignatureList.vue'
import { usePageTitle } from "@/composables/usePageTitle"

const { t } = useI18n()
usePageTitle('signatures.title')
const signatureStore = useSignatureStore()
const searchQuery = ref('')

const filteredSignatures = computed(() => {
  if (!searchQuery.value.trim()) {
    return signatureStore.userSignatures
  }

  const query = searchQuery.value.toLowerCase()
  return signatureStore.userSignatures.filter((sig: any) =>
    sig.docId.toLowerCase().includes(query) ||
    sig.docTitle?.toLowerCase().includes(query) ||
    sig.docUrl?.toLowerCase().includes(query)
  )
})

const activeSignatures = computed(() => {
  return filteredSignatures.value.filter((sig: any) => !sig.docDeletedAt)
})

const deletedSignatures = computed(() => {
  return filteredSignatures.value.filter((sig: any) => sig.docDeletedAt)
})

const uniqueDocumentsCount = computed(() => {
  const docIds = new Set(signatureStore.userSignatures.map((sig: any) => sig.docId))
  return docIds.size
})

const lastSignatureDate = computed(() => {
  if (signatureStore.userSignatures.length === 0) return null

  const latest = signatureStore.userSignatures[0]
  if (!latest) return null
  const date = new Date(latest.signedAt)
  return date.toLocaleDateString('fr-FR', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
})

function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString('fr-FR', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
}

async function refreshSignatures() {
  try {
    await Promise.all([
      signatureStore.fetchUserSignatures(),
      signatureStore.fetchPendingDocuments(),
    ])
  } catch (error) {
    console.error('Failed to refresh signatures:', error)
  }
}

onMounted(() => {
  refreshSignatures()
})
</script>

<template>
  <div class="min-h-[calc(100vh-8rem)]">
    <!-- Main Content -->
    <main class="mx-auto max-w-6xl px-4 sm:px-6 py-6 sm:py-8">
      <!-- Page Header -->
      <div class="mb-8">
        <h1 class="mb-2 text-2xl sm:text-3xl font-bold tracking-tight text-slate-900 dark:text-slate-50">
          {{ t('signatures.title') }}
        </h1>
        <p class="text-base sm:text-lg text-slate-500 dark:text-slate-400">
          {{ t('signatures.subtitle') }}
        </p>
      </div>

      <!-- Stats Pills Mobile -->
      <div class="sm:hidden mb-6 grid grid-cols-3 gap-3">
        <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-400" v-if="signatureStore.pendingDocuments.length > 0">
          <AlertTriangle :size="18" />
          <span class="text-xl font-bold">{{ signatureStore.pendingDocuments.length }}</span>
          <span class="text-xs whitespace-nowrap">{{ t('signatures.stats.pending') }}</span>
        </div>
        <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400">
          <FileSignature :size="18" />
          <span class="text-xl font-bold">{{ signatureStore.getUserSignaturesCount }}</span>
          <span class="text-xs whitespace-nowrap">{{ t('signatures.stats.total') }}</span>
        </div>
        <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-emerald-50 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400">
          <FileCheck :size="18" />
          <span class="text-xl font-bold">{{ uniqueDocumentsCount }}</span>
          <span class="text-xs whitespace-nowrap">{{ t('signatures.stats.unique') }}</span>
        </div>
        <div class="flex flex-col items-center justify-center gap-1 px-3 py-3 rounded-xl bg-blue-50 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400" v-if="signatureStore.pendingDocuments.length === 0">
          <Clock :size="18" />
          <span class="text-sm font-bold">{{ lastSignatureDate || t('signatures.stats.notAvailable') }}</span>
          <span class="text-xs whitespace-nowrap">{{ t('signatures.stats.last') }}</span>
        </div>
      </div>

      <!-- Stats Cards Desktop -->
      <div class="hidden sm:grid mb-8 gap-6 sm:grid-cols-2 lg:grid-cols-3" :class="{ 'lg:grid-cols-4': signatureStore.pendingDocuments.length > 0 }">
        <!-- Pending Documents -->
        <div v-if="signatureStore.pendingDocuments.length > 0" class="bg-white dark:bg-slate-800 rounded-xl border border-amber-200 dark:border-amber-700 p-5 hover:shadow-md transition-shadow">
          <div class="flex items-center space-x-4">
            <div class="w-12 h-12 rounded-xl bg-amber-50 dark:bg-amber-900/30 flex items-center justify-center">
              <AlertTriangle :size="24" class="text-amber-600 dark:text-amber-400" />
            </div>
            <div class="flex-1">
              <p class="text-sm font-medium text-slate-500 dark:text-slate-400">{{ t('signatures.stats.pendingDocuments') }}</p>
              <p class="text-2xl font-bold text-amber-600 dark:text-amber-400">
                {{ signatureStore.pendingDocuments.length }}
              </p>
            </div>
          </div>
        </div>

        <!-- Total Confirmations -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-5 hover:shadow-md transition-shadow">
          <div class="flex items-center space-x-4">
            <div class="w-12 h-12 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center">
              <FileSignature :size="24" class="text-blue-600 dark:text-blue-400" />
            </div>
            <div class="flex-1">
              <p class="text-sm font-medium text-slate-500 dark:text-slate-400">{{ t('signatures.stats.totalConfirmations') }}</p>
              <p class="text-2xl font-bold text-slate-900 dark:text-slate-100">
                {{ signatureStore.getUserSignaturesCount }}
              </p>
            </div>
          </div>
        </div>

        <!-- Unique Documents -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-5 hover:shadow-md transition-shadow">
          <div class="flex items-center space-x-4">
            <div class="w-12 h-12 rounded-xl bg-emerald-50 dark:bg-emerald-900/30 flex items-center justify-center">
              <FileCheck :size="24" class="text-emerald-600 dark:text-emerald-400" />
            </div>
            <div class="flex-1">
              <p class="text-sm font-medium text-slate-500 dark:text-slate-400">{{ t('signatures.stats.uniqueDocuments') }}</p>
              <p class="text-2xl font-bold text-slate-900 dark:text-slate-100">{{ uniqueDocumentsCount }}</p>
            </div>
          </div>
        </div>

        <!-- Last Confirmation -->
        <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700 p-5 hover:shadow-md transition-shadow">
          <div class="flex items-center space-x-4">
            <div class="w-12 h-12 rounded-xl bg-blue-50 dark:bg-blue-900/30 flex items-center justify-center">
              <Clock :size="24" class="text-blue-600 dark:text-blue-400" />
            </div>
            <div class="flex-1">
              <p class="text-sm font-medium text-slate-500 dark:text-slate-400">{{ t('signatures.stats.lastConfirmation') }}</p>
              <p class="text-lg font-semibold text-slate-900 dark:text-slate-100">
                {{ lastSignatureDate || t('signatures.stats.notAvailable') }}
              </p>
            </div>
          </div>
        </div>
      </div>

      <!-- Pending Documents Section -->
      <div v-if="signatureStore.pendingDocuments.length > 0" class="mb-6 bg-white dark:bg-slate-800 rounded-xl border border-amber-200 dark:border-amber-700">
        <div class="p-6 border-b border-amber-100 dark:border-amber-800">
          <div class="flex items-center gap-3">
            <AlertTriangle :size="20" class="text-amber-600 dark:text-amber-400" />
            <div>
              <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('signatures.pending.title') }}</h2>
              <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
                {{ t('signatures.pending.subtitle') }}
              </p>
            </div>
          </div>
        </div>
        <div class="p-6">
          <div v-if="signatureStore.loadingPending" class="flex justify-center py-4">
            <Loader2 :size="24" class="animate-spin text-amber-600" />
          </div>
          <div v-else class="space-y-3">
            <router-link
              v-for="doc in signatureStore.pendingDocuments"
              :key="doc.docId"
              :to="{ path: '/', query: { doc: doc.docId } }"
              class="flex items-center justify-between p-4 rounded-lg border border-slate-100 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-700/50 transition-colors group"
            >
              <div class="flex-1 min-w-0">
                <p class="font-medium text-slate-900 dark:text-slate-100 truncate group-hover:text-blue-600 dark:group-hover:text-blue-400 transition-colors">
                  {{ doc.title || doc.docId }}
                </p>
                <p class="text-sm text-slate-500 dark:text-slate-400 mt-0.5">
                  {{ t('signatures.pending.addedOn', { date: formatDate(doc.addedAt) }) }}
                </p>
              </div>
              <span class="ml-4 inline-flex items-center px-3 py-1 rounded-full text-xs font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 flex-shrink-0">
                {{ t('signatures.pending.awaitingConfirmation') }}
              </span>
            </router-link>
          </div>
        </div>
      </div>

      <!-- Signatures List Card -->
      <div class="bg-white dark:bg-slate-800 rounded-xl border border-slate-200 dark:border-slate-700">
        <!-- Header -->
        <div class="p-6 border-b border-slate-100 dark:border-slate-700">
          <div class="flex flex-col gap-4">
            <div class="flex items-center justify-between">
              <div>
                <h2 class="font-semibold text-slate-900 dark:text-slate-100">{{ t('signatures.allConfirmations') }}</h2>
                <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
                  {{ t('signatures.results', { count: filteredSignatures.length }) }}
                </p>
              </div>
            </div>

            <!-- Search -->
            <div class="relative">
              <Search :size="18" class="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none" />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('signatures.search')"
                class="w-full pl-10 pr-4 py-2.5 rounded-lg border border-slate-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-500 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              />
            </div>
          </div>
        </div>

        <!-- Content -->
        <div class="p-6">
          <!-- Loading -->
          <div v-if="signatureStore.loading" class="flex justify-center py-8">
            <Loader2 :size="32" class="animate-spin text-blue-600" />
          </div>

          <!-- Empty State -->
          <div v-else-if="filteredSignatures.length === 0" class="text-center py-12">
            <div class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-slate-100 dark:bg-slate-700">
              <FileSignature :size="28" class="text-slate-400" />
            </div>
            <p class="text-slate-500 dark:text-slate-400">{{ t('signatures.empty.alternative') }}</p>
          </div>

          <!-- Signatures List -->
          <div v-else class="space-y-4">
            <!-- Active signatures -->
            <SignatureList
              v-if="activeSignatures.length > 0"
              :signatures="activeSignatures"
              :loading="false"
              :show-user-info="false"
              :show-details="true"
              :show-actions="false"
              :is-deleted="false"
            />

            <!-- Deleted documents section header -->
            <div v-if="deletedSignatures.length > 0" class="py-4">
              <hr v-if="activeSignatures.length > 0" class="border-slate-200 dark:border-slate-700" />
              <p class="text-center text-sm text-slate-500 dark:text-slate-400 mt-4 mb-2">
                {{ t('signatures.deletedDocuments') }}
              </p>
            </div>

            <!-- Deleted signatures -->
            <SignatureList
              v-if="deletedSignatures.length > 0"
              :signatures="deletedSignatures"
              :loading="false"
              :show-user-info="false"
              :show-details="true"
              :show-actions="false"
              :is-deleted="true"
            />
          </div>
        </div>
      </div>

      <!-- Info Card -->
      <div class="mt-6 accent-border bg-blue-50 dark:bg-blue-900/20 rounded-r-lg p-4">
        <div class="flex items-start">
          <Info :size="20" class="mr-3 mt-0.5 text-blue-600 dark:text-blue-400 flex-shrink-0" />
          <div class="flex-1">
            <h3 class="mb-2 font-medium text-blue-900 dark:text-blue-200">{{ t('signatures.about.title') }}</h3>
            <p class="text-sm text-blue-800 dark:text-blue-300">
              {{ t('signatures.about.description') }}
            </p>
          </div>
        </div>
      </div>
    </main>
  </div>
</template>

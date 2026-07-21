// SPDX-License-Identifier: AGPL-3.0-or-later
import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const HomePage = () => import('@/pages/HomePage.vue')
const SignaturesPage = () => import('@/pages/SignaturesPage.vue')
const MyDocumentsPage = () => import('@/pages/MyDocumentsPage.vue')
const DocumentEditPage = () => import('@/pages/DocumentEditPage.vue')
const AuthChoicePage = () => import('@/pages/AuthChoicePage.vue')
const ReminderConfirmPage = () => import('@/pages/ReminderConfirmPage.vue')
const AdminDashboard = () => import('@/pages/admin/AdminDashboard.vue')
const AdminDocumentDetail = () => import('@/pages/admin/AdminDocumentDetail.vue')
const AdminWebhooks = () => import('@/pages/admin/AdminWebhooks.vue')
const AdminWebhookEdit = () => import('@/pages/admin/AdminWebhookEdit.vue')
const AdminSettings = () => import('@/pages/admin/AdminSettings.vue')
const EmbedPage = () => import('@/pages/EmbedPage.vue')
const NotFoundPage = () => import('@/pages/NotFoundPage.vue')

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'home',
    component: HomePage,
    meta: { requiresAuth: false }
  },
  {
    path: '/auth',
    name: 'auth-choice',
    component: AuthChoicePage,
    meta: { requiresAuth: false }
  },
  {
    path: '/auth/reminder',
    name: 'reminder-confirm',
    component: ReminderConfirmPage,
    meta: { requiresAuth: false }
  },
  {
    path: '/signatures',
    name: 'signatures',
    component: SignaturesPage,
    meta: { requiresAuth: true }
  },
  {
    path: '/documents',
    name: 'my-documents',
    component: MyDocumentsPage,
    meta: { requiresAuth: true }
  },
  {
    path: '/documents/:id',
    name: 'document-edit',
    component: DocumentEditPage,
    meta: { requiresAuth: true }
  },
  {
    path: '/admin',
    name: 'admin',
    component: AdminDashboard,
    meta: { requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/admin/webhooks',
    name: 'admin-webhooks',
    component: AdminWebhooks,
    meta: { requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/admin/webhooks/new',
    name: 'admin-webhook-new',
    component: AdminWebhookEdit,
    meta: { requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/admin/webhooks/:id',
    name: 'admin-webhook-edit',
    component: AdminWebhookEdit,
    meta: { requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/admin/docs/:docId',
    name: 'admin-document',
    component: AdminDocumentDetail,
    meta: { requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/admin/settings',
    name: 'admin-settings',
    component: AdminSettings,
    meta: { requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/embed',
    name: 'embed',
    component: EmbedPage,
    meta: { requiresAuth: false, isEmbed: true }
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: NotFoundPage
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    if (savedPosition) {
      return savedPosition
    }
    return { top: 0, behavior: 'smooth' }
  }
})

router.beforeEach(async (to, from, next) => {
  const authStore = useAuthStore()

  try {
    // Only check auth if the route requires it
    if (to.meta.requiresAuth || to.meta.requiresAdmin) {
      if (!authStore.initialized) {
        await authStore.checkAuth()
      }

      if (!authStore.isAuthenticated) {
        sessionStorage.setItem('redirectAfterLogin', to.fullPath)
        next({ name: 'auth-choice', query: { redirect: to.fullPath } })
        return
      }

      if (to.meta.requiresAdmin && !authStore.isAdmin) {
        next({ name: 'home' })
        return
      }
    }

    if (from.path === '/api/v1/auth/callback') {
      const redirectPath = sessionStorage.getItem('redirectAfterLogin')
      if (redirectPath) {
        sessionStorage.removeItem('redirectAfterLogin')
        next(redirectPath)
        return
      }
    }

    next()
  } catch (error) {
    console.error('Navigation guard error:', error)
    next()
  }
})

export default router

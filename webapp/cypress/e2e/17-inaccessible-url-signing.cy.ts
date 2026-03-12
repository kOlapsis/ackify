// SPDX-License-Identifier: AGPL-3.0-or-later
/// <reference types="cypress" />

describe('Test 17: Inaccessible URL — signing deadlock fix', () => {
  const adminEmail = 'admin@test.com'
  const signerEmail = 'signer-deadlock@example.com'

  beforeEach(() => {
    cy.clearCookies()
    // @ts-ignore
    cy.clearMailbox()
  })

  it('backend forces external mode when URL is inaccessible at creation', () => {
    // @ts-ignore
    cy.loginAsAdmin()

    // Create a document via API with a private IP URL
    // The backend should detect the URL is inaccessible and force readMode to external
    cy.request({
      method: 'POST',
      url: '/api/v1/documents',
      body: {
        reference: 'https://192.168.1.100/internal-policy-' + Date.now() + '.pdf',
        read_mode: 'integrated',
        require_full_read: true,
        allow_download: true,
      },
    }).then((response) => {
      expect(response.status).to.eq(201)
      const doc = response.body.data

      // Backend should have forced external mode
      expect(doc.read_mode).to.eq('external')
      expect(doc.require_full_read).to.eq(false)
      expect(doc.allow_download).to.eq(false)
    })
  })

  it('user can sign when integrated viewer fails to load document', () => {
    // @ts-ignore
    cy.loginAsAdmin()

    // Step 1: Create a document with a reachable URL first (so backend doesn't force external)
    const docRef = 'deadlock-test-' + Date.now()
    cy.request({
      method: 'POST',
      url: '/api/v1/documents',
      body: {
        reference: docRef,
        title: 'Deadlock Test Document',
      },
    }).then((createResp) => {
      expect(createResp.status).to.eq(201)
      const docId = createResp.body.data.doc_id

      // Step 2: Force the document to integrated mode with requireFullRead via admin API
      // and set an inaccessible URL (simulates a document that was created before the fix)
      cy.request({
        method: 'PUT',
        url: `/api/v1/admin/documents/${docId}/metadata`,
        body: {
          url: 'https://10.0.0.1/unreachable-doc.pdf',
          read_mode: 'integrated',
          require_full_read: true,
          allow_download: false,
        },
      }).then((updateResp) => {
        expect(updateResp.status).to.eq(200)

        // Step 3: Login as a regular signer
        cy.logout()
        // @ts-ignore
        cy.loginViaMagicLink(signerEmail)

        // Step 4: Visit the document page
        cy.visitWithLocale(`/?doc=${docId}`)

        // Step 5: The integrated viewer will fail → fallback block should appear
        // It should show the local network message since isIntegratedMode && documentLoadFailed
        cy.contains(/Local network document|Document sur réseau local/, { timeout: 15000 })
          .should('be.visible')

        // Step 6: The "read required" warning should NOT block (bypass when viewer fails)
        cy.contains(/Please read the entire|Veuillez lire l'intégralité/).should('not.exist')

        // Step 7: User should be able to check the certify checkbox and sign
        // @ts-ignore
        cy.confirmReading()

        // Step 8: Verify signature was recorded
        cy.contains(/Reading confirmed|Lecture confirmée/, { timeout: 10000 }).should('be.visible')
      })
    })
  })

  it('external mode documents with URL show correct fallback and are signable', () => {
    // @ts-ignore
    cy.loginViaMagicLink(signerEmail + '.ext')

    // Create a document via find-or-create with an inaccessible URL
    // The backend will force external mode
    const url = 'https://172.16.0.1/company-handbook-' + Date.now() + '.pdf'
    cy.visit(`/?doc=${encodeURIComponent(url)}`)

    // Should show external document section (not local network since readMode is external now)
    cy.contains(/External document|Document externe/, { timeout: 15000 }).should('be.visible')

    // Should display the URL
    cy.contains('172.16.0.1').should('be.visible')

    // Should be signable
    // @ts-ignore
    cy.confirmReading()
    cy.contains(/Reading confirmed|Lecture confirmée/, { timeout: 10000 }).should('be.visible')
  })
})

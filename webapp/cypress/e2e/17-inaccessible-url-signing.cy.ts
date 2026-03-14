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

    // Use find-or-create which returns readMode in its response
    const url = 'https://192.168.1.100/internal-policy-' + Date.now() + '.pdf'
    cy.request(`/api/v1/documents/find-or-create?doc=${encodeURIComponent(url)}`).then(
      (response) => {
        expect(response.status).to.eq(200)
        const doc = response.body.data
        expect(doc.isNew).to.eq(true)

        // Backend should have forced external mode
        expect(doc.readMode).to.eq('external')
        expect(doc.requireFullRead).to.eq(false)
        expect(doc.allowDownload).to.eq(false)
      },
    )
  })

  it('user can sign when integrated viewer fails to load document', () => {
    // @ts-ignore
    cy.loginAsAdmin()

    // Get CSRF token
    cy.request('/api/v1/csrf').then((csrfResponse) => {
      const csrfToken = csrfResponse.body.data.token

      // Step 1: Create a document with a simple reference (no URL, so no accessibility check)
      const docRef = 'deadlock-test-' + Date.now()
      cy.request({
        method: 'POST',
        url: '/api/v1/documents',
        headers: {
          'X-CSRF-Token': csrfToken,
        },
        body: {
          reference: docRef,
          title: 'Deadlock Test Document',
        },
      }).then((createResp) => {
        expect(createResp.status).to.eq(201)
        const docId = createResp.body.data.docId

        // Step 2: Force integrated mode with requireFullRead and inaccessible URL via admin API
        cy.request({
          method: 'PUT',
          url: `/api/v1/admin/documents/${docId}/metadata`,
          headers: {
            'X-CSRF-Token': csrfToken,
          },
          body: {
            url: 'https://10.0.0.1/unreachable-doc.pdf',
            readMode: 'integrated',
            requireFullRead: true,
            allowDownload: false,
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
          cy.contains(/Local network document|Document sur réseau local/, { timeout: 15000 })
            .should('be.visible')

          // Step 6: The "read required" warning should NOT block (bypass when viewer fails)
          cy.contains(/Please read the entire|Veuillez lire l'intégralité/).should('not.exist')

          // Step 7: User should be able to check the certify checkbox and sign
          // @ts-ignore
          cy.confirmReading()

          // Step 8: Verify signature was recorded
          cy.contains(/Reading confirmed|Lecture confirmée/, { timeout: 10000 }).should(
            'be.visible',
          )
        })
      })
    })
  })

  it('external mode documents with URL show correct fallback and are signable', () => {
    // @ts-ignore
    cy.loginViaMagicLink(signerEmail + '.ext')

    // Visit with an inaccessible URL — find-or-create will force external mode
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

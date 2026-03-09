# Admin Guide

Complete guide for administrators using Ackify to manage documents, expected signers, and email reminders.

## Table of Contents

- [Getting Admin Access](#getting-admin-access)
- [Admin Dashboard](#admin-dashboard)
- [Document Management](#document-management)
- [Expected Signers](#expected-signers)
- [Email Reminders](#email-reminders)
- [Monitoring & Statistics](#monitoring--statistics)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

---

## Getting Admin Access

### Prerequisites

To access admin features, your email must be configured in the `ACKIFY_ADMIN_EMAILS` environment variable.

```bash
# In .env file
ACKIFY_ADMIN_EMAILS=admin@company.com,manager@company.com
```

**After adding your email:**
1. Restart Ackify: `docker compose restart ackify-ce`
2. Log out and log in again
3. You should now see "Admin" link in the navigation

### Verify Admin Access

Visit `/admin` - if you see the admin dashboard, you have admin access.

---

## Admin Dashboard

**URL**: `/admin`

The admin dashboard provides:
- **Total Documents**: Number of documents in the system
- **Expected Readers**: Total number of expected signers across all documents
- **Active Documents**: Documents that are not soft-deleted
- **Document List**: Paginated list (20 per page) with search

### Dashboard Features

#### Quick Stats
Three KPI cards at the top:
- Total documents count
- Total expected readers/signers
- Active documents (non-deleted)

#### Document Search
- Search by title, document ID, or URL
- Real-time filtering

#### Document List
**Desktop view** - Table with columns:
- Document ID
- Title
- URL
- Created date
- Creator
- Actions (View details)

**Mobile view** - Card layout with:
- Document ID and title
- Creation info
- Tap to view details

#### Pagination
- 20 documents per page
- Previous/Next buttons
- Current page indicator

---

## Document Management

### Creating a Document

**From Admin Dashboard:**
1. Click "Create New Document" button
2. Fill in the form:
   - **Reference** (required): URL, file path, or custom ID
   - **Title** (optional): Auto-generated from URL if empty
   - **Description** (optional): Additional context
3. Click "Create Document"

**Automatic Features:**
- **Unique ID Generation**: Collision-resistant base36 doc_id
- **Title Extraction**: Auto-extracts from URL if not provided
- **Checksum Calculation**: For remote URLs (if admin and file < 10MB)

**Example:**
```
Reference: https://docs.company.com/policy-2025.pdf
Title: Security Policy 2025 (auto-extracted or manual)
Description: Annual security compliance policy
```

**Result:**
- doc_id: `k7m2n4p8` (auto-generated)
- Checksum: Auto-calculated SHA-256 (if URL is accessible)

### Viewing Document Details

**URL**: `/admin/docs/{docId}`

Provides comprehensive document information:

#### 1. **Metadata Section**
Edit document information:
- Title
- URL
- Description
- Checksum (SHA-256, SHA-512, or MD5)
- Checksum Algorithm

**To edit:**
1. Click "Edit Metadata" button
2. Modify fields
3. Click "Save Changes"
4. Confirmation modal for critical changes (checksum, algorithm)

#### 2. **Statistics Panel**
Real-time signature tracking:
- **Expected**: Number of expected signers
- **Signed**: Number who have signed
- **Pending**: Not yet signed
- **Completion**: Percentage complete

#### 3. **Expected Signers Section**
Lists all expected signers with status:
- **Email**: Signer's email address
- **Status**: ✅ Signed or ⏳ Pending
- **Added**: Date when added to expected list
- **Days Since Added**: Time tracking
- **Last Reminder**: When last reminder was sent
- **Reminder Count**: Total reminders sent
- **Actions**: Remove signer button

**Color coding:**
- Green background: Signer has signed
- Default background: Pending signature

#### 4. **Unexpected Signatures**
Shows users who signed but weren't on expected list:
- User email
- Signed date
- Indicates organic/unexpected participation

#### 5. **Actions**
- **Send Reminders**: Email pending signers
- **Share Link**: Generate and copy signing link
- **Delete Document**: Soft delete (preserves signature history)

### Updating Document Metadata

**Important fields:**

**Title & Description:**
- Can be changed freely
- No confirmation required

**URL:**
- Updates where document is located
- Confirmation modal shown

**Checksum & Algorithm:**
- Critical for integrity verification
- Confirmation modal warns of impact
- Change only if document version changed

**Workflow:**
1. Click "Edit Metadata"
2. Modify desired fields
3. Click "Save Changes"
4. If checksum/algorithm changed, confirm in modal
5. Success notification displayed

### Deleting a Document

**Soft Delete Behavior:**
- Document marked as deleted (`deleted_at` timestamp set)
- Signature history preserved
- Document no longer appears in public lists
- Admin can still view via direct URL
- Signatures CASCADE update (marked with `doc_deleted_at`)

**To delete:**
1. Go to document detail page (`/admin/docs/{docId}`)
2. Click "Delete Document" button
3. Confirm deletion in modal
4. Document moved to deleted state

**Note**: There is no "undelete" - this is permanent soft delete.

---

## Expected Signers

Expected signers are users you want to track for document completion.

### Adding Expected Signers

**From document detail page:**
1. Scroll to "Expected Signers" section
2. Click "Add Expected Signer" button
3. Enter email address(es):
   - Single: `alice@company.com`
   - Multiple: Comma-separated `alice@company.com,bob@company.com`
4. Optionally add notes
5. Click "Add"

**API endpoint:**
```http
POST /api/v1/admin/documents/{docId}/signers
Content-Type: application/json
X-CSRF-Token: {token}

{
  "emails": ["alice@company.com", "bob@company.com"],
  "notes": "Board members - Q1 2025"
}
```

**Constraints:**
- Email must be valid format
- UNIQUE constraint: Cannot add same email twice to same document
- Added by current admin user (tracked in `added_by`)

### Removing Expected Signers

**From document detail page:**
1. Find signer in Expected Signers list
2. Click "Remove" button next to their email
3. Confirm removal

**API endpoint:**
```http
DELETE /api/v1/admin/documents/{docId}/signers/{email}
X-CSRF-Token: {token}
```

**Effect:**
- Signer removed from expected list
- Does NOT delete their signature if they already signed
- Reminder history preserved in `reminder_logs`

### Tracking Completion Status

**Document Status API:**
```http
GET /api/v1/admin/documents/{docId}/status
```

**Response:**
```json
{
  "docId": "abc123",
  "expectedCount": 10,
  "signedCount": 7,
  "pendingCount": 3,
  "completionPercentage": 70.0
}
```

**Visual indicators:**
- Progress bar showing completion percentage
- Color-coded status: Green (signed), Orange (pending)
- Days since added (helps identify slow signers)

---

## Email Reminders

Email reminders are sent asynchronously via the `email_queue` system.

### Sending Reminders

**From document detail page:**
1. Click "Send Reminders" button
2. Modal opens with options:
   - **Send to**: All pending OR specific emails
   - **Document URL**: Pre-filled, can customize
   - **Language**: en, fr, es, de, it
3. Click "Send Reminders"
4. Confirmation: "X reminders queued for sending"

**API endpoint:**
```http
POST /api/v1/admin/documents/{docId}/reminders
Content-Type: application/json
X-CSRF-Token: {token}

{
  "emails": ["alice@company.com"],  // Optional: specific emails
  "docURL": "https://docs.company.com/policy.pdf",
  "locale": "en"
}
```

**Behavior:**
- Sends to ALL pending signers if `emails` not specified
- Sends to specific `emails` if provided (even if already signed)
- Emails queued in `email_queue` table
- Background worker processes queue
- Retry on failure (3 attempts, exponential backoff)

### Email Templates

**Location**: `backend/templates/emails/`

**Available templates:**
- `reminder.html` - HTML version
- `reminder.txt` - Plain text version

**Variables available in templates:**
- `{{.DocTitle}}` - Document title
- `{{.DocURL}}` - Document URL
- `{{.RecipientEmail}}` - Recipient's email
- `{{.SenderName}}` - Admin who sent reminder
- `{{.OrganisationName}}` - From ACKIFY_ORGANISATION

**Locales**: en, fr, es, de, it
- Template directory: `templates/emails/{locale}/`
- Fallback to default locale if translation missing

### Reminder History

**View reminder log:**
```http
GET /api/v1/admin/documents/{docId}/reminders
```

**Response:**
```json
{
  "reminders": [
    {
      "id": 123,
      "docId": "abc123",
      "recipientEmail": "alice@company.com",
      "sentAt": "2025-01-15T10:30:00Z",
      "sentBy": "admin@company.com",
      "templateUsed": "reminder",
      "status": "sent",
      "errorMessage": null
    }
  ]
}
```

**Status values:**
- `queued` - In email_queue, not yet processed
- `sent` - Successfully delivered
- `failed` - Delivery failed (check errorMessage)
- `bounced` - Email bounced back

**Tracking:**
- Last reminder sent date shown per signer
- Reminder count shown per signer
- Helps avoid over-sending

### Email Queue Monitoring

**Check queue status (PostgreSQL):**
```sql
-- Pending emails
SELECT id, to_addresses, subject, status, scheduled_for
FROM email_queue
WHERE status IN ('pending', 'processing')
ORDER BY priority DESC, scheduled_for ASC;

-- Failed emails
SELECT id, to_addresses, last_error, retry_count
FROM email_queue
WHERE status = 'failed';
```

**Worker configuration:**
- Batch size: 10 emails
- Poll interval: 5 seconds
- Max retries: 3
- Cleanup: 7 days retention

---

## Monitoring & Statistics

### Document-Level Statistics

**Completion tracking:**
- Expected vs Signed counts
- Pending signer list
- Completion percentage
- Average time to sign

**Reminder effectiveness:**
- Reminders sent count
- Success/failure rates
- Time between reminder and signature

### System-Wide Metrics

**PostgreSQL queries:**

```sql
-- Total documents
SELECT COUNT(*) FROM documents WHERE deleted_at IS NULL;

-- Total signatures
SELECT COUNT(*) FROM signatures;

-- Documents by completion status
SELECT
  CASE
    WHEN signed_count = expected_count THEN '100%'
    WHEN signed_count >= expected_count * 0.75 THEN '75-99%'
    WHEN signed_count >= expected_count * 0.50 THEN '50-74%'
    ELSE '<50%'
  END as completion_bracket,
  COUNT(*) as doc_count
FROM (
  SELECT
    d.doc_id,
    COUNT(DISTINCT es.email) as expected_count,
    COUNT(DISTINCT s.user_email) as signed_count
  FROM documents d
  LEFT JOIN expected_signers es ON d.doc_id = es.doc_id
  LEFT JOIN signatures s ON d.doc_id = s.doc_id AND s.user_email = es.email
  WHERE d.deleted_at IS NULL
  GROUP BY d.doc_id
) stats
GROUP BY completion_bracket;

-- Email queue statistics
SELECT status, COUNT(*), MIN(created_at), MAX(created_at)
FROM email_queue
GROUP BY status;
```

### Export Data

**Signatures for a document:**
```sql
COPY (
  SELECT s.user_email, s.user_name, s.signed_at, s.payload_hash
  FROM signatures s
  WHERE s.doc_id = 'your_doc_id'
  ORDER BY s.signed_at
) TO '/tmp/signatures_export.csv' WITH CSV HEADER;
```

**Expected signers status:**
```sql
COPY (
  SELECT
    es.email,
    CASE WHEN s.id IS NOT NULL THEN 'Signed' ELSE 'Pending' END as status,
    es.added_at,
    s.signed_at
  FROM expected_signers es
  LEFT JOIN signatures s ON es.doc_id = s.doc_id AND es.email = s.user_email
  WHERE es.doc_id = 'your_doc_id'
) TO '/tmp/expected_signers_export.csv' WITH CSV HEADER;
```

---

## Best Practices

### 1. Document Creation

✅ **Do:**
- Use descriptive titles
- Add clear descriptions
- Include document URL for easy access
- Store checksum for integrity verification
- Create expected signers list before sharing

❌ **Don't:**
- Use generic titles like "Document 1"
- Leave URL empty if document is accessible online
- Change checksums unless document actually changed

### 2. Expected Signers Management

✅ **Do:**
- Add expected signers before sending document link
- Use clear notes to explain why signers are expected
- Review pending signers regularly
- Remove signers who are no longer relevant

❌ **Don't:**
- Add hundreds of signers at once (use batches)
- Send reminders too frequently (max once per week)
- Remove signers who have already signed (preserve history)

### 3. Email Reminders

✅ **Do:**
- Wait 3-5 days before first reminder
- Send in recipient's preferred language
- Include clear document title and URL
- Track reminder history to avoid spam
- Send reminders during business hours

❌ **Don't:**
- Send daily reminders (causes fatigue)
- Send without checking if already signed
- Use generic subjects (personalize with doc title)
- Send outside business hours

### 4. Data Integrity

✅ **Do:**
- Regularly backup PostgreSQL database
- Verify checksums match actual documents
- Monitor email queue for failures
- Review unexpected signatures (may indicate broader interest)
- Export important signature data

❌ **Don't:**
- Delete documents with active signatures
- Modify timestamps manually in database
- Ignore failed email deliveries
- Change checksums without updating the document

### 5. Security

✅ **Do:**
- Limit admin access to trusted users only
- Use HTTPS in production (`ACKIFY_BASE_URL=https://...`)
- Rotate `ACKIFY_OAUTH_COOKIE_SECRET` periodically
- Monitor admin actions via application logs
- Use OAuth allowed domain restrictions

❌ **Don't:**
- Share admin credentials
- Run without HTTPS in production
- Disable CSRF protection
- Ignore authentication failures in logs

---

## Troubleshooting

### Common Issues

#### 1. Admin Link Not Visible

**Problem**: Can't see "Admin" link in navigation

**Solutions:**
- Verify email in `ACKIFY_ADMIN_EMAILS` environment variable
- Restart Ackify: `docker compose restart ackify-ce`
- Log out and log back in
- Check logs: `docker compose logs ackify-ce | grep admin`

#### 2. Emails Not Sending

**Problem**: Reminders queued but not delivered

**Diagnosis:**
```sql
SELECT * FROM email_queue WHERE status = 'failed' ORDER BY created_at DESC LIMIT 10;
```

**Solutions:**
- Check SMTP configuration (`ACKIFY_MAIL_HOST`, `ACKIFY_MAIL_USERNAME`, etc.)
- Verify SMTP credentials are correct
- Check email worker logs: `docker compose logs ackify-ce | grep email`
- Ensure `ACKIFY_MAIL_FROM` is valid sender address
- Test SMTP connection manually

#### 3. Duplicate Signer Error

**Problem**: "Email already exists as expected signer"

**Cause**: UNIQUE constraint on (doc_id, email)

**Solution**: This is expected behavior - each email can only be added once per document

#### 4. Checksum Mismatch

**Problem**: Users report checksum doesn't match

**Solutions:**
- Verify stored checksum matches actual document
- Check algorithm used (SHA-256, SHA-512, MD5)
- Recalculate checksum and update via Edit Metadata
- Ensure users are downloading correct version

#### 5. Document Not Appearing

**Problem**: Created document doesn't show in list

**Solutions:**
- Check if document was soft-deleted (`deleted_at IS NOT NULL`)
- Verify creation succeeded (check response/logs)
- Clear browser cache
- Check database: `SELECT * FROM documents WHERE doc_id = 'your_id';`

#### 6. Signature Already Exists

**Problem**: User can't sign document again

**Cause**: UNIQUE constraint (doc_id, user_sub) - one signature per user per document

**Solution**: This is expected - users cannot sign the same document twice

### Getting Help

**Logs:**
```bash
# Application logs
docker compose logs -f ackify-ce

# Database logs
docker compose logs -f ackify-db

# Email worker logs (grep email)
docker compose logs ackify-ce | grep -i email
```

**Database inspection:**
```bash
# Connect to PostgreSQL
docker compose exec ackify-db psql -U ackifyr ackify

# Useful queries
SELECT * FROM documents ORDER BY created_at DESC LIMIT 10;
SELECT * FROM expected_signers WHERE doc_id = 'your_doc_id';
SELECT * FROM email_queue WHERE status != 'sent' ORDER BY created_at DESC;
```

**Report issues:**
- GitHub: https://github.com/kolapsis/ackify/issues
- Include logs and error messages
- Describe expected vs actual behavior

---

## Quick Reference

### Environment Variables
```bash
ACKIFY_ADMIN_EMAILS=admin@company.com
ACKIFY_MAIL_HOST=smtp.gmail.com
ACKIFY_MAIL_FROM=noreply@company.com
```

### Key Endpoints
```
GET  /admin                              # Dashboard
GET  /admin/docs/{docId}                 # Document detail
POST /admin/documents/{docId}/signers    # Add signer
POST /admin/documents/{docId}/reminders  # Send reminders
PUT  /admin/documents/{docId}/metadata   # Update metadata
```

### Important Tables
- `documents` - Document metadata
- `signatures` - User signatures
- `expected_signers` - Who should sign
- `reminder_logs` - Email history
- `email_queue` - Async email queue

### Keyboard Shortcuts (Frontend)
- Search bar auto-focus on dashboard
- Enter to submit forms
- Esc to close modals

---

**Last Updated**: 2025-10-26
**Version**: 1.0.0

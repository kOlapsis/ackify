-- Normalize expected-signer and reminder emails to lowercase so they match
-- signatures (always stored lowercase) case-insensitively, and enforce
-- case-insensitive uniqueness at the database level.
--
-- The reminder_logs -> expected_signers foreign key references
-- expected_signers(doc_id, email); PostgreSQL checks it with NO ACTION at the
-- end of each statement, so the parent email cannot be rewritten while children
-- still reference the old value. We therefore drop the FK, normalize both
-- tables, then recreate it.

ALTER TABLE reminder_logs DROP CONSTRAINT reminder_logs_doc_id_recipient_email_fkey;

-- Remove case-insensitive duplicate expected signers, keeping the earliest row
-- (smallest id) per (doc_id, lower(email)). Any reminder_logs children are
-- reconnected to the surviving row once both tables are lowercased.
DELETE FROM expected_signers a
USING expected_signers b
WHERE a.doc_id = b.doc_id
  AND LOWER(a.email) = LOWER(b.email)
  AND a.id > b.id;

UPDATE expected_signers
SET email = LOWER(email)
WHERE email <> LOWER(email);

UPDATE reminder_logs
SET recipient_email = LOWER(recipient_email)
WHERE recipient_email <> LOWER(recipient_email);

ALTER TABLE reminder_logs
    ADD CONSTRAINT reminder_logs_doc_id_recipient_email_fkey
    FOREIGN KEY (doc_id, recipient_email)
    REFERENCES expected_signers(doc_id, email)
    ON DELETE CASCADE;

-- Database-level guarantee that emails stay case-insensitively unique per
-- document, independent of the application normalizing input.
CREATE UNIQUE INDEX idx_expected_signers_doc_lower_email
    ON expected_signers (doc_id, LOWER(email));

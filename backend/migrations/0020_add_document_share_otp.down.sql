-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Rollback migration 0014: Remove OTP document share columns

DROP INDEX IF EXISTS idx_magic_link_tokens_revoked;
DROP INDEX IF EXISTS idx_magic_link_tokens_doc_purpose;

ALTER TABLE magic_link_tokens
  DROP COLUMN IF EXISTS shared_by,
  DROP COLUMN IF EXISTS revoked_at,
  DROP COLUMN IF EXISTS otp_max_attempts,
  DROP COLUMN IF EXISTS otp_attempts,
  DROP COLUMN IF EXISTS otp_hash;

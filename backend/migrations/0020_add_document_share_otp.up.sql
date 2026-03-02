-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Migration 0014: Add OTP support for document sharing via magic links
-- Adds columns for OTP hash, attempt tracking, revocation, and share origin

ALTER TABLE magic_link_tokens
  ADD COLUMN otp_hash TEXT,
  ADD COLUMN otp_attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN otp_max_attempts INT NOT NULL DEFAULT 5,
  ADD COLUMN revoked_at TIMESTAMPTZ,
  ADD COLUMN shared_by TEXT;

-- Index for listing shares per document
CREATE INDEX idx_magic_link_tokens_doc_purpose ON magic_link_tokens(doc_id, purpose)
  WHERE doc_id IS NOT NULL;

-- Index for filtering out revoked tokens
CREATE INDEX idx_magic_link_tokens_revoked ON magic_link_tokens(revoked_at)
  WHERE revoked_at IS NOT NULL;

COMMENT ON COLUMN magic_link_tokens.otp_hash IS 'Bcrypt hash of the 6-digit OTP (NULL for non-OTP tokens)';
COMMENT ON COLUMN magic_link_tokens.otp_attempts IS 'Number of failed OTP verification attempts';
COMMENT ON COLUMN magic_link_tokens.otp_max_attempts IS 'Maximum allowed OTP attempts before lockout (default: 5)';
COMMENT ON COLUMN magic_link_tokens.revoked_at IS 'Timestamp when the share was revoked (NULL = active)';
COMMENT ON COLUMN magic_link_tokens.shared_by IS 'Email of the admin who created the document share';

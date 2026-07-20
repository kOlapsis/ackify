-- The lowercasing of existing emails is not reversible (original casing is lost);
-- this only drops the case-insensitive uniqueness index added by the up migration.
DROP INDEX IF EXISTS idx_expected_signers_doc_lower_email;

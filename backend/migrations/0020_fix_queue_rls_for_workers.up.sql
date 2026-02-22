-- SPDX-License-Identifier: AGPL-3.0-or-later

-- ============================================================================
-- Migration: Fix RLS policies for background workers on queue tables
-- ============================================================================
-- Background workers (email worker, webhook worker) don't have app.tenant_id
-- set in their session, so current_tenant_id() returns NULL. This causes
-- RLS policies to filter out ALL rows, preventing workers from processing
-- queued items.
--
-- Fix: Split the single ALL policy into per-command policies:
--   - SELECT/UPDATE/DELETE: allow cross-tenant when no tenant is set (workers)
--   - INSERT: strict tenant isolation (only app with tenant context can insert)
--
-- Safety: the trigger prevent_tenant_id_modification() already prevents
-- changing tenant_id on UPDATE, so workers can't reassign rows.
-- ============================================================================

-- ----- EMAIL_QUEUE -----
DROP POLICY IF EXISTS tenant_isolation_email_queue ON email_queue;

CREATE POLICY email_queue_select ON email_queue
    FOR SELECT
    USING (
        current_tenant_id() IS NULL
        OR tenant_id = current_tenant_id()
    );

CREATE POLICY email_queue_insert ON email_queue
    FOR INSERT
    WITH CHECK (tenant_id = current_tenant_id());

CREATE POLICY email_queue_update ON email_queue
    FOR UPDATE
    USING (
        current_tenant_id() IS NULL
        OR tenant_id = current_tenant_id()
    )
    WITH CHECK (
        current_tenant_id() IS NULL
        OR tenant_id = current_tenant_id()
    );

CREATE POLICY email_queue_delete ON email_queue
    FOR DELETE
    USING (tenant_id = current_tenant_id());

-- ----- WEBHOOK_DELIVERIES -----
DROP POLICY IF EXISTS tenant_isolation_webhook_deliveries ON webhook_deliveries;

CREATE POLICY webhook_deliveries_select ON webhook_deliveries
    FOR SELECT
    USING (
        current_tenant_id() IS NULL
        OR tenant_id = current_tenant_id()
    );

CREATE POLICY webhook_deliveries_insert ON webhook_deliveries
    FOR INSERT
    WITH CHECK (tenant_id = current_tenant_id());

CREATE POLICY webhook_deliveries_update ON webhook_deliveries
    FOR UPDATE
    USING (
        current_tenant_id() IS NULL
        OR tenant_id = current_tenant_id()
    )
    WITH CHECK (
        current_tenant_id() IS NULL
        OR tenant_id = current_tenant_id()
    );

CREATE POLICY webhook_deliveries_delete ON webhook_deliveries
    FOR DELETE
    USING (tenant_id = current_tenant_id());

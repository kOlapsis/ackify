-- SPDX-License-Identifier: AGPL-3.0-or-later

-- Restore original single ALL policies (strict tenant isolation)

DROP POLICY IF EXISTS email_queue_select ON email_queue;
DROP POLICY IF EXISTS email_queue_insert ON email_queue;
DROP POLICY IF EXISTS email_queue_update ON email_queue;
DROP POLICY IF EXISTS email_queue_delete ON email_queue;

CREATE POLICY tenant_isolation_email_queue ON email_queue
    USING (tenant_id = current_tenant_id())
    WITH CHECK (tenant_id = current_tenant_id());

DROP POLICY IF EXISTS webhook_deliveries_select ON webhook_deliveries;
DROP POLICY IF EXISTS webhook_deliveries_insert ON webhook_deliveries;
DROP POLICY IF EXISTS webhook_deliveries_update ON webhook_deliveries;
DROP POLICY IF EXISTS webhook_deliveries_delete ON webhook_deliveries;

CREATE POLICY tenant_isolation_webhook_deliveries ON webhook_deliveries
    USING (tenant_id = current_tenant_id())
    WITH CHECK (tenant_id = current_tenant_id());

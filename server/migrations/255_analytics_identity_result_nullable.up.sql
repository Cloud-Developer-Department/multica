-- Engineering analytics (CLO-239) review follow-up (P2-04): L1 recent_events
-- result may be null for offboard events per API-CLO-228 v2.0 §L1
-- (`"result": "success"|"failed"|null`). The 254 table forced NOT NULL; relax
-- the column so offboard (and any lifecycle event without a result) can carry
-- SQL NULL, which the handler surfaces as JSON null.

ALTER TABLE identity_import ALTER COLUMN result DROP NOT NULL;

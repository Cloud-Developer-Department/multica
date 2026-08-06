-- Engineering analytics platform data model (CLO-239) — rollback.
DROP INDEX IF EXISTS idx_member_ws_department;
DROP INDEX IF EXISTS idx_identity_import_ws_occurred;
DROP INDEX IF EXISTS idx_vcs_author_mapping_ws;
DROP INDEX IF EXISTS idx_deployment_event_ws_finished;
DROP INDEX IF EXISTS idx_repo_quality_snapshot_ws_repo;

DROP TABLE IF EXISTS identity_import;
DROP TABLE IF EXISTS vcs_author_mapping;
DROP TABLE IF EXISTS deployment_event;
DROP TABLE IF EXISTS repo_quality_snapshot;

ALTER TABLE member DROP COLUMN IF EXISTS department;

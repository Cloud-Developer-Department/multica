-- Engineering analytics platform data model (CLO-239).
--
-- The four-dashboard analytics platform (Adoption & Activity, Agent
-- Performance, Git Contributions, DORA) reads most of its metrics from
-- existing tables (issue / agent_task_queue / comment / skill /
-- agent_skill / member / user / activity_log / vcs_pull_request /
-- issue_vcs_pull_request). This migration adds ONLY the integration
-- surfaces the platform's external-data modules need:
--
--   * member.department       — department slicing (L2 + department_id on
--                               Tab1/Tab2). Populated by LDAP/IDP sync or a
--                               manual mapping. Empty until then: L2 returns
--                               source_status.ready=false and passing a
--                               department_id returns 400 (E18).
--   * repo_quality_snapshot   — code-quality radar (G2). Written by the
--                               scan-tool ingestion (SonarQube/Semgrep/CI
--                               reports). Empty until then: G2 guide state.
--   * deployment_event        — DORA deployments (D1 deploy / D2). Written
--                               by the deployment-pipeline webhook / file
--                               import / manual entry (feasibility §8).
--   * vcs_author_mapping      — Git commit author -> member/agent mapping
--                               for ELOC attribution (G1). Empty until then:
--                               G1 returns unmapped authors.
--   * identity_import         — manual lifecycle events (L1): onboard /
--                               offboard / cutoff rows a deploy engineer or
--                               LDAP sync writes. Empty until then: L1 guide.
--
-- These tables carry no foreign keys, matching the VCS tables' convention:
-- cross-table cleanup is handled in application code (the analytics module
-- never deletes, only reads; workspaces own their rows via workspace_id).

-- Department slice on members (department_id = external ID or manual stable ID).
ALTER TABLE member ADD COLUMN IF NOT EXISTS department TEXT;

CREATE TABLE IF NOT EXISTS repo_quality_snapshot (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    repo             TEXT NOT NULL,
    coverage         NUMERIC,
    vulnerabilities  INTEGER,
    duplication_rate NUMERIC,
    snapshot_at      TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, repo, snapshot_at)
);

CREATE TABLE IF NOT EXISTS deployment_event (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    deployment_id TEXT NOT NULL,
    app           TEXT,
    env           TEXT,
    started_at    TIMESTAMPTZ NOT NULL,
    finished_at   TIMESTAMPTZ NOT NULL,
    result        TEXT NOT NULL CHECK (result IN ('success', 'failed')),
    recovered_at  TIMESTAMPTZ,
    issue_ids     JSONB NOT NULL DEFAULT '[]',
    sha           TEXT,
    reason        TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS vcs_author_mapping (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    author       TEXT NOT NULL,
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('member', 'agent')),
    entity_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, author)
);

CREATE TABLE IF NOT EXISTS identity_import (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    event_type   TEXT NOT NULL CHECK (event_type IN ('onboard', 'offboard', 'cutoff')),
    member_name  TEXT NOT NULL,
    department   TEXT,
    occurred_at  TIMESTAMPTZ NOT NULL,
    result       TEXT NOT NULL CHECK (result IN ('success', 'failed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_repo_quality_snapshot_ws_repo ON repo_quality_snapshot(workspace_id, repo, snapshot_at);
CREATE INDEX IF NOT EXISTS idx_deployment_event_ws_finished ON deployment_event(workspace_id, finished_at);
CREATE INDEX IF NOT EXISTS idx_vcs_author_mapping_ws ON vcs_author_mapping(workspace_id);
CREATE INDEX IF NOT EXISTS idx_identity_import_ws_occurred ON identity_import(workspace_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_member_ws_department ON member(workspace_id, department);

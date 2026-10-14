// Wire types for the F-523 (CLO-617) marketplace capability
// (/api/marketplace/*). These mirror the Go structs in
// server/internal/handler/marketplace.go and the contract in PRD §4.
//
// The listing template payload IS the portable resource template document
// (ResourceTemplateDoc) — the marketplace introduces no second template
// protocol.

export type MarketplaceKind = "agent" | "squad";

export type MarketplaceSort = "latest" | "downloads" | "name";

export interface MarketplaceListing {
  id: string;
  kind: MarketplaceKind;
  title: string;
  summary: string;
  category: string;
  tags: string[];
  version: string;
  author_id: string;
  author_display_name: string;
  source_workspace_id: string;
  template_id: string;
  status: "published" | "archived";
  downloads: number;
  installs: number;
  created_at: string;
  updated_at: string;
  /** Only present on detail responses (PRD R5: the list never carries it). */
  template?: unknown;
}

export interface MarketplacePublishMetadata {
  title: string;
  summary?: string;
  category?: string;
  tags?: string[];
  version?: string;
}

export interface PublishMarketplaceListingRequest {
  kind: MarketplaceKind;
  resource_id?: string;
  template?: unknown;
  metadata: MarketplacePublishMetadata;
}

export interface ListMarketplaceListingsResponse {
  items: MarketplaceListing[];
  total: number;
  page: number;
  page_size: number;
}

export interface MarketplaceDetailResponse {
  listing: MarketplaceListing;
}

export interface ReportMarketplaceDownloadResponse {
  downloads: number;
}

/** Server-side category whitelist (PRD §2.4). */
export const MARKETPLACE_CATEGORIES = [
  "content-creation",
  "dev-programming",
  "data-analysis",
  "ai-agent",
  "knowledge-management",
  "business-ops",
  "education",
  "professional",
  "it-ops-security",
  "life-service",
  "other",
] as const;

export type MarketplaceCategory = (typeof MARKETPLACE_CATEGORIES)[number];

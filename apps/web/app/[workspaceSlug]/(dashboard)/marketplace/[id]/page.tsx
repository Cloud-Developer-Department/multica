"use client";

import { use } from "react";
import { MarketplaceDetailPage } from "@multica/views/marketplace";

export default function MarketplaceDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <MarketplaceDetailPage id={id} />;
}

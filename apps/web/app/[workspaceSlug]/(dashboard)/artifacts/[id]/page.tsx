"use client";

import { use } from "react";
import { ArtifactDetailPage } from "@multica/views/artifacts/components";

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return <ArtifactDetailPage artifactId={id} />;
}

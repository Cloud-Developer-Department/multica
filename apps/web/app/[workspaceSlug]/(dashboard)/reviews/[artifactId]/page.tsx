"use client";

import { use } from "react";
import { ReviewDetailPage } from "@multica/views/reviews/components";

export default function Page({ params }: { params: Promise<{ artifactId: string }> }) {
  const { artifactId } = use(params);
  return <ReviewDetailPage artifactId={artifactId} />;
}

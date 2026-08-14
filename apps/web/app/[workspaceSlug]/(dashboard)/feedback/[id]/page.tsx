"use client";

import { use } from "react";
import { FeedbackDetailPage } from "@multica/views/feedback";

export default function FeedbackDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <FeedbackDetailPage feedbackId={id} />;
}

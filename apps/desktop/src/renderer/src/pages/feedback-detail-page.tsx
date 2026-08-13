import { useParams } from "react-router-dom";
import { FeedbackDetailPage as SharedFeedbackDetailPage } from "@multica/views/feedback";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function FeedbackDetailPage() {
  const { id } = useParams<{ id: string }>();

  useDocumentTitle("Feedback");

  if (!id) return null;
  return <SharedFeedbackDetailPage feedbackId={id} />;
}

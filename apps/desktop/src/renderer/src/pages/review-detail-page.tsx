import { useParams } from "react-router-dom";
import { ReviewDetailPage } from "@multica/views/reviews/components";

export function DesktopReviewDetailPage() {
  const { artifactId } = useParams<{ artifactId: string }>();
  if (!artifactId) return null;
  return <ReviewDetailPage artifactId={artifactId} />;
}

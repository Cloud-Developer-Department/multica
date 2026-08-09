import { useParams } from "react-router-dom";
import { ArtifactDetailPage } from "@multica/views/artifacts/components";

export function DesktopArtifactDetailPage() {
  const { id } = useParams<{ id: string }>();
  if (!id) return null;
  return <ArtifactDetailPage artifactId={id} />;
}

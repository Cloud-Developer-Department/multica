import { useParams } from "react-router-dom";
import { WorkflowDetailPage } from "@multica/views/workflows/components";

export function DesktopWorkflowDetailPage() {
  const { id } = useParams<{ id: string }>();
  if (!id) return null;
  return <WorkflowDetailPage workflowId={id} />;
}

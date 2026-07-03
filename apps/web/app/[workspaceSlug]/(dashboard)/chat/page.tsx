"use client";

import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { RoomsPage } from "@multica/views/rooms";

export default function Page() {
  return (
    <ErrorBoundary>
      <RoomsPage />
    </ErrorBoundary>
  );
}

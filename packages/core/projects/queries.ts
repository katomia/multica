import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const projectKeys = {
  all: (wsId: string) => ["projects", wsId] as const,
  list: (wsId: string) => [...projectKeys.all(wsId), "list"] as const,
  // Distinct key (not a child of `list`) so invalidating the regular project
  // list does not silently refetch the without_room picker list, and vice versa.
  withoutRoom: (wsId: string) => [...projectKeys.all(wsId), "without-room"] as const,
  detail: (wsId: string, id: string) =>
    [...projectKeys.all(wsId), "detail", id] as const,
};

export function projectListOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.list(wsId),
    queryFn: () => api.listProjects(),
    select: (data) => data.projects,
  });
}

// Projects that do not yet have a project-based room. Powers the
// "Create room from project" picker in the chat page.
export function projectsWithoutRoomOptions(wsId: string) {
  return queryOptions({
    queryKey: projectKeys.withoutRoom(wsId),
    queryFn: () => api.listProjects({ without_room: true }),
    select: (data) => data.projects,
    staleTime: 30 * 1000,
  });
}

export function projectDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: projectKeys.detail(wsId, id),
    queryFn: () => api.getProject(id),
  });
}

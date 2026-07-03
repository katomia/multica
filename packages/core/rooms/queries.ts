import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const roomKeys = {
  all: (wsId: string) => ["workspaces", wsId, "rooms"] as const,
  list: (wsId: string) => [...roomKeys.all(wsId), "list"] as const,
  detail: (wsId: string, roomId: string) =>
    [...roomKeys.all(wsId), "detail", roomId] as const,
  members: (wsId: string, roomId: string) =>
    [...roomKeys.detail(wsId, roomId), "members"] as const,
  messages: (wsId: string, roomId: string) =>
    [...roomKeys.detail(wsId, roomId), "messages"] as const,
  orchestrations: (wsId: string, roomId: string) =>
    [...roomKeys.detail(wsId, roomId), "orchestrations"] as const,
  issues: (wsId: string, roomId: string) =>
    [...roomKeys.detail(wsId, roomId), "issues"] as const,
  resourceGrants: (wsId: string, roomId: string) =>
    [...roomKeys.detail(wsId, roomId), "resource-grants"] as const,
};

export function roomsOptions(wsId: string) {
  return queryOptions({
    queryKey: roomKeys.list(wsId),
    queryFn: () => api.listRooms(),
    staleTime: 30 * 1000,
  });
}

export function roomMembersOptions(wsId: string, roomId: string) {
  return queryOptions({
    queryKey: roomKeys.members(wsId, roomId),
    queryFn: () => api.listRoomMembers(roomId),
    enabled: Boolean(roomId),
    staleTime: 30 * 1000,
  });
}

export function roomMessagesOptions(wsId: string, roomId: string) {
  return queryOptions({
    queryKey: roomKeys.messages(wsId, roomId),
    queryFn: () => api.listRoomMessages(roomId),
    enabled: Boolean(roomId),
    staleTime: 5 * 1000,
  });
}

export function roomIssuesOptions(wsId: string, roomId: string) {
  return queryOptions({
    queryKey: roomKeys.issues(wsId, roomId),
    queryFn: () => api.listRoomIssues(roomId),
    enabled: Boolean(roomId),
    staleTime: 10 * 1000,
  });
}

export function roomOrchestrationsOptions(wsId: string, roomId: string) {
  return queryOptions({
    queryKey: roomKeys.orchestrations(wsId, roomId),
    queryFn: () => api.listRoomOrchestrations(roomId),
    enabled: Boolean(roomId),
    staleTime: 5 * 1000,
  });
}

export function roomResourceGrantsOptions(wsId: string, roomId: string) {
  return queryOptions({
    queryKey: roomKeys.resourceGrants(wsId, roomId),
    queryFn: () => api.listRoomResourceGrants(roomId),
    enabled: Boolean(roomId),
    staleTime: 10 * 1000,
  });
}

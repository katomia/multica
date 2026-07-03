import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { projectKeys } from "../projects/queries";
import type { AddRoomMemberRequest, CreateRoomRequest, RoomMessage, RoomResourceGrant, SendRoomMessageRequest, UpdateRoomResourceGrantRequest } from "../types";
import { roomKeys } from "./queries";

function createClientMessageId() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export function useCreateRoom() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateRoomRequest) => api.createRoom(data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: roomKeys.list(wsId) });
      // A project-based room consumes a project from the "Create from project"
      // picker list — invalidate so the picker no longer offers it.
      qc.invalidateQueries({ queryKey: projectKeys.withoutRoom(wsId) });
    },
  });
}

export function useAddRoomMember(roomId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: AddRoomMemberRequest) => api.addRoomMember(roomId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: roomKeys.members(wsId, roomId) });
    },
  });
}

export function useSendRoomMessage(roomId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: SendRoomMessageRequest) =>
      api.sendRoomMessage(roomId, { ...data, id: data.id ?? createClientMessageId() }),
    onSuccess: (res) => {
      qc.setQueryData<RoomMessage[]>(roomKeys.messages(wsId, roomId), (old) => {
        const next = [...(old ?? [])];
        const upsert = (msg: RoomMessage | undefined) => {
          if (!msg) return;
          const idx = next.findIndex((item) => item.id === msg.id);
          if (idx >= 0) next[idx] = msg;
          else next.push(msg);
        };
        upsert(res.message);
        upsert(res.system_message);
        return next;
      });
      qc.invalidateQueries({ queryKey: roomKeys.issues(wsId, roomId) });
      qc.invalidateQueries({ queryKey: issueKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.myAll(wsId) });
    },
  });
}

export function useUpdateRoomResourceGrant(roomId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      grantId,
      data,
    }: {
      grantId: string;
      data: UpdateRoomResourceGrantRequest;
    }) => api.updateRoomResourceGrant(roomId, grantId, data),
    // Optimistic: flip the access_level in the cached grant list immediately so
    // the toggle feels instant; roll back on error.
    onMutate: async ({ grantId, data }) => {
      await qc.cancelQueries({ queryKey: roomKeys.resourceGrants(wsId, roomId) });
      const prev = qc.getQueryData<RoomResourceGrant[]>(roomKeys.resourceGrants(wsId, roomId));
      qc.setQueryData<RoomResourceGrant[]>(roomKeys.resourceGrants(wsId, roomId), (old) =>
        old
          ? old.map((g) => (g.id === grantId ? { ...g, access_level: data.access_level } : g))
          : old,
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(roomKeys.resourceGrants(wsId, roomId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: roomKeys.resourceGrants(wsId, roomId) });
    },
  });
}

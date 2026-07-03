"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Bot, CheckCircle2, FolderGit, FolderOpen, Hash, Loader2, MessageSquarePlus, Plus, Send, Users } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectResourcesOptions, projectsWithoutRoomOptions } from "@multica/core/projects";
import { useAddRoomMember, useCreateRoom, useSendRoomMessage, useUpdateRoomResourceGrant } from "@multica/core/rooms/mutations";
import { roomIssuesOptions, roomKeys, roomMembersOptions, roomMessagesOptions, roomOrchestrationsOptions, roomResourceGrantsOptions, roomsOptions } from "@multica/core/rooms/queries";
import type { Project, ProjectResource, Room, RoomIssueEntry, RoomOrchestration, RoomResourceGrant, SendRoomMessageResponse } from "@multica/core/types";
import { useWSEvent } from "@multica/core/realtime";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";

const EMPTY_ROOMS: Room[] = [];
const EMPTY_ENTRIES: RoomIssueEntry[] = [];
const EMPTY_ORCHESTRATIONS: RoomOrchestration[] = [];
const EMPTY_RESOURCES: ProjectResource[] = [];
const EMPTY_GRANTS: RoomResourceGrant[] = [];
const EMPTY_PROJECTS: Project[] = [];

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function roomTitle(room: Room) {
  return room.display_name || room.name || "Room";
}

// slugifyRoomName turns a project title into a room name identifier. Room names
// are unique per workspace, so a stable slug derived from the title is both
// human-readable and unlikely to collide with hand-created rooms.
function slugifyRoomName(title: string) {
  return title.trim().toLowerCase().replace(/[\s_]+/g, "-").replace(/[^a-z0-9-]/g, "").replace(/-+/g, "-").replace(/^-|-$/g, "") || `project-${Date.now()}`;
}

function resourceDisplay(resource: ProjectResource) {
  if (resource.resource_type === "github_repo") {
    const ref = resource.resource_ref as { url?: string; ref?: string };
    return resource.label || (ref.ref ? `${ref.url} @ ${ref.ref}` : ref.url) || resource.resource_type;
  }
  if (resource.resource_type === "local_directory") {
    const ref = resource.resource_ref as { local_path?: string; label?: string };
    return resource.label || ref.label || ref.local_path || resource.resource_type;
  }
  return resource.label || resource.resource_type;
}

function orchestrationSummary(orch: RoomOrchestration, issueCount: number) {
  if (orch.display_status?.trim()) return orch.display_status;
  if (orch.status === "failed") return orch.error?.trim() || "orchestrator failed";
  if (issueCount > 0) return issueCount === 1 ? "orchestrator created issue" : `orchestrator created ${issueCount} issues`;
  return "orchestrator queued";
}

function orchestrationTone(orch: RoomOrchestration) {
  if (orch.display_tone === "failed") {
    return {
      icon: AlertCircle,
      className: "border-destructive/30 bg-destructive/5 text-destructive",
      badge: "failed",
    };
  }
  if (orch.display_tone === "warning") {
    return {
      icon: AlertCircle,
      className: "border-warning/30 bg-warning/10 text-warning",
      badge: "attention",
    };
  }
  if (orch.display_tone === "applied") {
    return {
      icon: CheckCircle2,
      className: "border-emerald-500/30 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300",
      badge: "live",
    };
  }
  return {
    icon: Loader2,
    className: "border-border bg-muted/50 text-muted-foreground",
    badge: "pending",
  };
}

export function RoomsPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [selectedRoomId, setSelectedRoomId] = useState<string | null>(null);
  const [newRoomName, setNewRoomName] = useState("");
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [draft, setDraft] = useState("");
  const [, setLastResult] = useState<SendRoomMessageResponse | null>(null);
  const [mentionAnchor, setMentionAnchor] = useState<{ query: string; start: number } | null>(null);
  const [selectedMentionIndex, setSelectedMentionIndex] = useState(0);
  const [createFromProjectOpen, setCreateFromProjectOpen] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const qc = useQueryClient();

  const roomsQuery = useQuery(roomsOptions(wsId));
  const rooms = roomsQuery.data ?? EMPTY_ROOMS;
  const activeRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomId) ?? rooms[0] ?? null,
    [rooms, selectedRoomId],
  );

  useEffect(() => {
    if (!selectedRoomId && rooms[0]) {
      setSelectedRoomId(rooms[0].id);
    }
  }, [rooms, selectedRoomId]);

  const roomId = activeRoom?.id ?? "";
  const activeProjectId = activeRoom?.project_id ?? null;
  const membersQuery = useQuery(roomMembersOptions(wsId, roomId));
  const messagesQuery = useQuery(roomMessagesOptions(wsId, roomId));
  const orchestrationsQuery = useQuery(roomOrchestrationsOptions(wsId, roomId));
  const roomIssuesQuery = useQuery(roomIssuesOptions(wsId, roomId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  // Project resources for the active room's bound project (lower-left panel).
  // Disabled when the room is a plain room (no project_id).
  const projectResourcesQuery = useQuery({
    ...projectResourcesOptions(wsId, activeProjectId ?? ""),
    enabled: Boolean(activeProjectId),
  });
  // Projects without a project-based room — the "Create from project" picker
  // list. Only fetched when the picker is open to avoid hammering the endpoint.
  const projectsWithoutRoomQuery = useQuery({
    ...projectsWithoutRoomOptions(wsId),
    enabled: createFromProjectOpen,
  });
  // Resource access grants for the active room (right-aside management). Only
  // meaningful for project-based rooms.
  const resourceGrantsQuery = useQuery({
    ...roomResourceGrantsOptions(wsId, roomId),
    enabled: Boolean(roomId) && Boolean(activeProjectId),
  });
  const createRoom = useCreateRoom();
  const addMember = useAddRoomMember(roomId);
  const sendMessage = useSendRoomMessage(roomId);
  const updateGrant = useUpdateRoomResourceGrant(roomId);

  const members = membersQuery.data ?? [];
  const messages = messagesQuery.data ?? [];
  const orchestrations = orchestrationsQuery.data ?? EMPTY_ORCHESTRATIONS;
  const roomIssueEntries = roomIssuesQuery.data ?? EMPTY_ENTRIES;
  const agents = agentsQuery.data ?? [];
  const projectResources = projectResourcesQuery.data ?? EMPTY_RESOURCES;
  const projectsWithoutRoom = projectsWithoutRoomQuery.data ?? EMPTY_PROJECTS;
  const resourceGrants = resourceGrantsQuery.data ?? EMPTY_GRANTS;
  const joinedAgentIds = useMemo(
    () => new Set(members.filter((m) => m.member_type === "agent").map((m) => m.member_id)),
    [members],
  );
  const roomOrchestratorAgentIds = useMemo(
    () => new Set(rooms.map((room) => room.orchestrator_agent_id).filter((id): id is string => Boolean(id))),
    [rooms],
  );
  const senderNameMap = useMemo(
    () => new Map(members.map((m) => [m.member_id, m.name])),
    [members],
  );
  const getRoomSenderName = useCallback(
    (senderType: string, senderId: string | null | undefined) => {
      if (senderType === "system") return "System";
      if (senderType === "agent" && senderId && senderId === activeRoom?.orchestrator_agent_id) {
        return "Orchestrator";
      }
      return senderNameMap.get(senderId ?? "") ?? senderType;
    },
    [activeRoom?.orchestrator_agent_id, senderNameMap],
  );
  const orchestrationByMessageId = useMemo(
    () => new Map(orchestrations.map((orch) => [orch.source_message_id, orch])),
    [orchestrations],
  );
  const issueCountByMessageId = useMemo(() => {
    const counts = new Map<string, number>();
    for (const entry of roomIssueEntries) {
      counts.set(entry.link.room_message_id, (counts.get(entry.link.room_message_id) ?? 0) + 1);
    }
    return counts;
  }, [roomIssueEntries]);
  const joinableAgents = agents.filter(
    (agent) => !agent.archived_at && !joinedAgentIds.has(agent.id) && !roomOrchestratorAgentIds.has(agent.id),
  );

  const handleCreateRoom = () => {
    const name = newRoomName.trim();
    if (!name) return;
    createRoom.mutate(
      { name, display_name: name },
      {
        onSuccess: (room) => {
          setNewRoomName("");
          setSelectedRoomId(room.id);
        },
        onError: () => toast.error("Failed to create room"),
      },
    );
  };

  const handleCreateFromProject = (project: Project) => {
    const name = slugifyRoomName(project.title);
    createRoom.mutate(
      { name, display_name: project.title, project_id: project.id },
      {
        onSuccess: (room) => {
          setCreateFromProjectOpen(false);
          setSelectedRoomId(room.id);
        },
        onError: () => toast.error("Failed to create project room"),
      },
    );
  };

  const handleToggleGrant = (grant: RoomResourceGrant) => {
    const next = grant.access_level === "write" ? "read" : "write";
    updateGrant.mutate(
      { grantId: grant.id, data: { access_level: next } },
      { onError: () => toast.error("Failed to update resource access") },
    );
  };

  const handleAddAgent = () => {
    if (!selectedAgentId || !roomId) return;
    addMember.mutate(
      { member_type: "agent", member_id: selectedAgentId },
      {
        onSuccess: () => {
          setSelectedAgentId("");
        },
        onError: () => toast.error("Failed to add agent"),
      },
    );
  };

  const handleRoomMessageCreated = useCallback(
    (payload: unknown) => {
      const p = payload as { room_id?: string } | undefined;
      if (!p?.room_id || p.room_id !== roomId) return;
      qc.invalidateQueries({ queryKey: roomKeys.messages(wsId, roomId) });
      qc.invalidateQueries({ queryKey: roomKeys.orchestrations(wsId, roomId) });
      qc.invalidateQueries({ queryKey: roomKeys.issues(wsId, roomId) });
    },
    [qc, wsId, roomId],
  );
  useWSEvent("room:message_created", handleRoomMessageCreated);

  const roomAgentMembers = useMemo(
    () =>
      members.filter(
        (m) =>
          m.member_type === "agent" &&
          m.name &&
          m.member_id !== activeRoom?.orchestrator_agent_id,
      ),
    [activeRoom?.orchestrator_agent_id, members],
  );

  const mentionSuggestions = useMemo(() => {
    if (!mentionAnchor) return [];
    const q = mentionAnchor.query.toLowerCase();
    return roomAgentMembers.filter((m) => m.name?.toLowerCase().startsWith(q));
  }, [mentionAnchor, roomAgentMembers]);

  useEffect(() => {
    setSelectedMentionIndex(0);
  }, [mentionAnchor?.query, mentionSuggestions.length]);

  function detectMention(text: string, cursor: number) {
    const before = text.slice(0, cursor);
    const match = before.match(/@([^\s@,，:：一-鿿]*)$/);
    if (match) {
      setMentionAnchor({ query: match[1] ?? "", start: cursor - (match[0]?.length ?? 0) });
    } else {
      setMentionAnchor(null);
    }
  }

  function applyMention(name: string) {
    if (!mentionAnchor || !textareaRef.current) return;
    const cursor = textareaRef.current.selectionStart ?? draft.length;
    const before = draft.slice(0, mentionAnchor.start);
    const after = draft.slice(cursor);
    const next = `${before}@${name} ${after}`;
    setDraft(next);
    setMentionAnchor(null);
    requestAnimationFrame(() => {
      if (!textareaRef.current) return;
      textareaRef.current.focus();
      const pos = mentionAnchor.start + name.length + 2;
      textareaRef.current.setSelectionRange(pos, pos);
    });
  }

  const handleSend = () => {
    const content = draft.trim();
    if (!content || !roomId || sendMessage.isPending) return;
    setMentionAnchor(null);
    // Generate a stable client ID so the backend deduplicates on double-send.
    const clientId = crypto.randomUUID();
    setDraft("");
    sendMessage.mutate(
      { content, id: clientId },
      {
        onSuccess: (res) => {
          setLastResult(res);
        },
        onError: () => {
          setDraft(content);
          toast.error("Failed to send message");
        },
      },
    );
  };


  return (
    <div className="flex h-full min-h-0 bg-background">
      <aside className="flex w-72 shrink-0 flex-col border-r bg-muted/20">
        <div className="border-b p-4">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Hash className="size-4" />
            Rooms
          </div>
          <div className="mt-3 flex gap-2">
            <Input
              value={newRoomName}
              onChange={(event) => setNewRoomName(event.target.value)}
              placeholder="New room"
              onKeyDown={(event) => {
                if (event.key === "Enter") handleCreateRoom();
              }}
            />
            <Button size="icon" onClick={handleCreateRoom} disabled={createRoom.isPending || !newRoomName.trim()}>
              {createRoom.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
            </Button>
          </div>
          <Popover
            open={createFromProjectOpen}
            onOpenChange={setCreateFromProjectOpen}
          >
            <PopoverTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className="mt-2 w-full justify-start text-xs"
                  disabled={createRoom.isPending}
                >
                  <Plus className="size-3.5" />
                  Create from project
                </Button>
              }
            />
            <PopoverContent align="start" className="w-72 p-1">
              {projectsWithoutRoomQuery.isPending && (
                <div className="flex items-center gap-2 p-2 text-xs text-muted-foreground">
                  <Loader2 className="size-3.5 animate-spin" />
                  Loading projects...
                </div>
              )}
              {!projectsWithoutRoomQuery.isPending && projectsWithoutRoom.length === 0 && (
                <div className="p-2 text-xs text-muted-foreground">
                  No projects available. All projects already have a room, or create a project first.
                </div>
              )}
              <div className="max-h-64 overflow-y-auto">
                {projectsWithoutRoom.map((project) => (
                  <button
                    key={project.id}
                    type="button"
                    onClick={() => handleCreateFromProject(project)}
                    disabled={createRoom.isPending}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-accent"
                  >
                    <FolderGit className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">{project.title}</span>
                    {project.resource_count > 0 && (
                      <span className="text-[10px] text-muted-foreground">
                        {project.resource_count}
                      </span>
                    )}
                  </button>
                ))}
              </div>
            </PopoverContent>
          </Popover>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto border-b p-2">
          {roomsQuery.isPending && <div className="p-3 text-sm text-muted-foreground">Loading rooms...</div>}
          {!roomsQuery.isPending && rooms.length === 0 && (
            <div className="p-3 text-sm text-muted-foreground">Create a room to start shared workspace chat.</div>
          )}
          {rooms.map((room) => (
            <button
              key={room.id}
              type="button"
              onClick={() => {
                setSelectedRoomId(room.id);
                setLastResult(null);
              }}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent",
                activeRoom?.id === room.id && "bg-accent text-accent-foreground",
              )}
            >
              <Hash className="size-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{roomTitle(room)}</span>
              {room.project_id && (
                <Badge variant="outline" className="shrink-0 text-[10px]">project</Badge>
              )}
            </button>
          ))}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          <div className="flex items-center gap-2 px-1 text-xs font-medium text-muted-foreground">
            <FolderOpen className="size-3.5" />
            Project resources
          </div>
          {!activeProjectId && (
            <div className="mt-2 p-2 text-xs text-muted-foreground">
              Select a project-based room to see its resources here.
            </div>
          )}
          {activeProjectId && projectResourcesQuery.isPending && (
            <div className="mt-2 p-1 text-xs text-muted-foreground">Loading resources...</div>
          )}
          {activeProjectId && projectResources.length === 0 && !projectResourcesQuery.isPending && (
            <div className="mt-2 p-2 text-xs text-muted-foreground">
              This project has no resources yet.
            </div>
          )}
          <div className="mt-1 space-y-1">
            {projectResources.map((resource) => (
              <div
                key={resource.id}
                className="flex items-center gap-2 rounded-md px-1.5 py-1 text-xs"
                title={resourceDisplay(resource)}
              >
                {resource.resource_type === "local_directory" ? (
                  <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <FolderGit className="size-3.5 shrink-0 text-muted-foreground" />
                )}
                <span className="min-w-0 flex-1 truncate">{resourceDisplay(resource)}</span>
              </div>
            ))}
          </div>
        </div>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-16 shrink-0 items-center justify-between border-b px-5">
          <div className="min-w-0">
            <div className="flex items-center gap-2 font-medium">
              <Hash className="size-4" />
              <span className="truncate">{activeRoom ? roomTitle(activeRoom) : "Chat"}</span>
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              Shared room messages. `/issue` creates an unassigned issue; `@agent` asks the RoomOrchestrator.
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Users className="size-4 text-muted-foreground" />
            <span className="text-sm text-muted-foreground">{members.length}</span>
          </div>
        </header>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {!activeRoom && (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              Create or select a room.
            </div>
          )}
          {activeRoom && messagesQuery.isPending && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              Loading messages...
            </div>
          )}
          {activeRoom && messages.length === 0 && !messagesQuery.isPending && (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              Try `/issue 写一首诗到桌面` or `@agent 帮我检查这个任务`.
            </div>
          )}
          <div className="space-y-4">
            {messages.map((message) => (
              <div key={message.id} className="flex gap-3">
                <ActorAvatar
                  actorType={message.sender_type === "member" ? "member" : message.sender_type === "agent" ? "agent" : "system"}
                  actorId={message.sender_id ?? "system"}
                  size={32}
                  profileLink={message.sender_type !== "system"}
                  showStatusDot={message.sender_type === "agent"}
                />
                <div className="min-w-0 flex-1">
                  {(() => {
                    const orch = orchestrationByMessageId.get(message.id);
                    const issueCount = issueCountByMessageId.get(message.id) ?? 0;
                    const tone = orch ? orchestrationTone(orch) : null;
                    const ToneIcon = tone?.icon;
                    return (
                      <>
	                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
	                    <span className="font-medium text-foreground">
	                      {getRoomSenderName(message.sender_type, message.sender_id)}
	                    </span>
                    <span>{formatTime(message.created_at)}</span>
                    {message.message_type !== "human" && <Badge variant="secondary">{message.message_type}</Badge>}
                  </div>
                  <div className="mt-1 whitespace-pre-wrap break-words text-sm leading-6">{message.content}</div>
                        {orch && tone && ToneIcon && (
                          <div className={cn("mt-2 flex items-start gap-2 rounded-md border px-3 py-2 text-xs", tone.className)}>
                            <ToneIcon className={cn("mt-0.5 size-3.5 shrink-0", tone.badge === "pending" && "animate-spin")} />
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="font-medium">Task status</span>
                                <Badge variant="outline" className="h-5 px-1.5 text-[10px]">
                                  {tone.badge}
                                </Badge>
                              </div>
                              <div className="mt-1 break-words">
                                {orchestrationSummary(orch, issueCount)}
                              </div>
                            </div>
                          </div>
                        )}
                      </>
                    );
                  })()}
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="shrink-0 border-t p-4">
          <div className="relative flex gap-3">
            {mentionSuggestions.length > 0 && (
              <div className="absolute bottom-full left-0 z-50 mb-1 w-56 overflow-hidden rounded-md border bg-popover shadow-md">
                {mentionSuggestions.map((m, index) => (
                  <button
                    key={m.member_id}
                    type="button"
                    className={cn(
                      "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent",
                      index === selectedMentionIndex && "bg-accent text-accent-foreground",
                    )}
                    onMouseDown={(e) => {
                      e.preventDefault();
                      applyMention(m.name ?? "");
                    }}
                  >
                    <Bot className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="truncate font-medium">{m.name}</span>
                  </button>
                ))}
              </div>
            )}
            <Textarea
              ref={textareaRef}
              value={draft}
              onChange={(event) => {
                setDraft(event.target.value);
                detectMention(event.target.value, event.target.selectionStart ?? event.target.value.length);
              }}
              onKeyDown={(event) => {
                if (mentionSuggestions.length > 0) {
                  if (event.key === "ArrowDown") {
                    event.preventDefault();
                    setSelectedMentionIndex((index) => (index + 1) % mentionSuggestions.length);
                    return;
                  }
                  if (event.key === "ArrowUp") {
                    event.preventDefault();
                    setSelectedMentionIndex((index) => (index - 1 + mentionSuggestions.length) % mentionSuggestions.length);
                    return;
                  }
                  if (event.key === "Escape") {
                    event.preventDefault();
                    setMentionAnchor(null);
                    return;
                  }
                  if (event.key === "Enter" && !event.metaKey && !event.ctrlKey) {
                    const selected = mentionSuggestions[selectedMentionIndex] ?? mentionSuggestions[0];
                    if (selected) {
                      event.preventDefault();
                      applyMention(selected.name ?? "");
                      return;
                    }
                  }
                }
                if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
                  handleSend();
                }
              }}
              onSelect={(event) => {
                const el = event.currentTarget;
                detectMention(el.value, el.selectionStart ?? el.value.length);
              }}
              placeholder="/issue 写一个任务，或 @agent 发起编排判断"
              disabled={!activeRoom}
              className="min-h-20 resize-none"
            />
            <Button className="self-end" onClick={handleSend} disabled={!activeRoom || !draft.trim() || sendMessage.isPending}>
              {sendMessage.isPending ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />}
              Send
            </Button>
          </div>
        </div>
      </main>

      <aside className="flex w-96 shrink-0 flex-col border-l bg-muted/10">
        <div className="border-b p-4">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Bot className="size-4" />
            Room agents
          </div>
          <div className="mt-3 flex gap-2">
            <select
              value={selectedAgentId}
              onChange={(event) => setSelectedAgentId(event.target.value)}
              disabled={!activeRoom || joinableAgents.length === 0}
              className="h-9 min-w-0 flex-1 rounded-md border bg-background px-3 text-sm"
            >
              <option value="">Add agent...</option>
              {joinableAgents.map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {agent.name}
                </option>
              ))}
            </select>
            <Button onClick={handleAddAgent} disabled={!selectedAgentId || addMember.isPending}>
              {addMember.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
              Add
            </Button>
          </div>
          <div className="mt-3 flex flex-wrap gap-1.5">
            {members.filter((m) => m.member_type === "agent").map((member) => (
              <Badge key={`${member.member_type}:${member.member_id}`} variant="secondary">
                @{member.name}
              </Badge>
            ))}
            {members.filter((m) => m.member_type === "agent").length === 0 && (
              <span className="text-xs text-muted-foreground">Add an agent before using @agent.</span>
            )}
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          <div className="flex items-center gap-2 text-sm font-medium">
            <MessageSquarePlus className="size-4" />
            Room issues
          </div>
          <div className="mt-3 space-y-2">
            {roomIssueEntries.length === 0 && (
              <div className="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
                Issues created from this room will appear here.
              </div>
            )}
            {roomIssueEntries.map((entry) => (
              <AppLink
                key={`${entry.link.room_message_id}:${entry.link.issue_id}`}
                href={paths.issueDetail(entry.link.issue_id)}
                className="block rounded-md border bg-background p-3 text-sm hover:bg-accent"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{entry.issue?.identifier ?? "Issue"}</span>
                  <div className="flex items-center gap-1">
                    {entry.issue && <Badge variant="outline">{entry.issue.status}</Badge>}
                    <Badge variant="secondary" className="text-xs">{entry.link.link_role}</Badge>
                  </div>
                </div>
                <div className="mt-1 line-clamp-2 text-muted-foreground">
                  {entry.issue?.title ?? entry.link.issue_id}
                </div>
              </AppLink>
            ))}
          </div>

          {activeProjectId && (
            <div className="mt-6">
              <div className="flex items-center gap-2 text-sm font-medium">
                <FolderGit className="size-4" />
                Resource access
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                Each agent's access to the project's resources. Default is write; click to downgrade to read.
              </div>
              <div className="mt-3 space-y-1.5">
                {resourceGrantsQuery.isPending && (
                  <div className="text-xs text-muted-foreground">Loading grants...</div>
                )}
                {!resourceGrantsQuery.isPending && resourceGrants.length === 0 && (
                  <div className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">
                    Add an agent to seed default write grants for the project's resources.
                  </div>
                )}
                {resourceGrants.map((grant) => (
                  <div
                    key={grant.id}
                    className="flex items-center gap-2 rounded-md border bg-background px-2 py-1.5 text-xs"
                  >
                    <Badge variant="secondary" className="shrink-0">@{grant.agent_name}</Badge>
                    <span className="min-w-0 flex-1 truncate" title={grant.resource_label || grant.resource_type}>
                      {grant.resource_label || grant.resource_type}
                    </span>
                    <button
                      type="button"
                      onClick={() => handleToggleGrant(grant)}
                      disabled={updateGrant.isPending}
                      title="Toggle access level"
                      className={cn(
                        "shrink-0 rounded-sm px-1.5 py-0.5 text-[10px] font-medium transition-colors",
                        grant.access_level === "write"
                          ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-500/20"
                          : "bg-muted text-muted-foreground hover:bg-accent",
                      )}
                    >
                      {grant.access_level === "write" ? "write" : "read"}
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </aside>
    </div>
  );
}

import type { Issue } from "./issue";

export type RoomVisibility = "workspace" | "private";
export type RoomMemberType = "member" | "agent";
export type RoomSenderType = "member" | "agent" | "system";
export type RoomMessageType =
  | "human"
  | "agent"
  | "system"
  | "orchestration"
  | "issue_created"
  | "issue_status";

export interface Room {
  id: string;
  workspace_id: string;
  name: string;
  display_name: string;
  description: string;
  visibility: RoomVisibility;
  created_by_id: string;
  orchestrator_agent_id?: string | null;
  // project_id is set when this is a project-based room (one per project).
  // Nullable + optional so installed clients parse older backends that predate
  // the column (API compatibility: defensive optional-chain on server fields).
  project_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface RoomMember {
  room_id: string;
  member_type: RoomMemberType;
  member_id: string;
  name: string;
  avatar_url?: string | null;
  joined_by_id: string | null;
  joined_at: string;
}

export interface RoomMessage {
  id: string;
  room_id: string;
  sender_type: RoomSenderType;
  sender_id: string | null;
  message_type: RoomMessageType;
  content: string;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface RoomOrchestration {
  id: string;
  room_id: string;
  source_message_id: string;
  decision_source: string;
  decision_type: string;
  status: string;
  decision_json: Record<string, unknown>;
  error: string | null;
  created_at: string;
  applied_at: string | null;
  display_status: string;
  display_tone: string;
}

export interface RoomIssueLink {
  room_id: string;
  room_message_id: string;
  orchestration_id: string | null;
  issue_id: string;
  link_role: "root" | "child" | "related" | string;
  created_at: string;
}

export interface RoomIssueEntry {
  link: RoomIssueLink;
  issue?: Issue;
}

export interface CreateRoomRequest {
  name: string;
  display_name?: string;
  description?: string;
  // project_id, when set, makes this a project-based room: it inherits the
  // project's resources as shared context and is uniqueness-bound to that
  // project (one project-based room per project).
  project_id?: string;
}

export interface AddRoomMemberRequest {
  member_type: RoomMemberType;
  member_id: string;
}

export interface SendRoomMessageRequest {
  id?: string;
  content: string;
}

export interface SendRoomMessageResponse {
  message: RoomMessage;
  orchestration?: RoomOrchestration;
  issues: Issue[];
  links: RoomIssueLink[];
  system_message?: RoomMessage;
}

// RoomResourceAccessLevel is the per-(agent, resource) permission in a
// project-based room. Default is "write" (read + mutate the resource); admins
// can downgrade to "read" so an agent can still see the resource as context
// but cannot modify it.
export type RoomResourceAccessLevel = "read" | "write";

// RoomResourceGrant is one cell of the room access matrix. The server enriches
// each row with agent_name + resource meta (type/label) so the UI can render
// the matrix without extra round-trips.
export interface RoomResourceGrant {
  id: string;
  room_id: string;
  resource_id: string;
  agent_id: string;
  agent_name: string;
  resource_type: string;
  resource_label: string;
  access_level: RoomResourceAccessLevel;
  created_at: string;
  updated_at: string;
}

export interface UpdateRoomResourceGrantRequest {
  access_level: RoomResourceAccessLevel;
}

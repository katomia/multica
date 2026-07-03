package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

type RoomResponse struct {
	ID                  string  `json:"id"`
	WorkspaceID         string  `json:"workspace_id"`
	Name                string  `json:"name"`
	DisplayName         string  `json:"display_name"`
	Description         string  `json:"description"`
	Visibility          string  `json:"visibility"`
	CreatedByID         string  `json:"created_by_id"`
	OrchestratorAgentID *string `json:"orchestrator_agent_id,omitempty"`
	ProjectID           *string `json:"project_id,omitempty"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type RoomMemberResponse struct {
	RoomID     string  `json:"room_id"`
	MemberType string  `json:"member_type"`
	MemberID   string  `json:"member_id"`
	Name       string  `json:"name"`
	AvatarURL  *string `json:"avatar_url,omitempty"`
	JoinedByID *string `json:"joined_by_id"`
	JoinedAt   string  `json:"joined_at"`
}

type RoomMessageResponse struct {
	ID          string         `json:"id"`
	RoomID      string         `json:"room_id"`
	SenderType  string         `json:"sender_type"`
	SenderID    *string        `json:"sender_id"`
	MessageType string         `json:"message_type"`
	Content     string         `json:"content"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   string         `json:"created_at"`
}

type RoomOrchestrationResponse struct {
	ID              string         `json:"id"`
	RoomID          string         `json:"room_id"`
	SourceMessageID string         `json:"source_message_id"`
	DecisionSource  string         `json:"decision_source"`
	DecisionType    string         `json:"decision_type"`
	Status          string         `json:"status"`
	DecisionJSON    map[string]any `json:"decision_json"`
	Error           *string        `json:"error"`
	CreatedAt       string         `json:"created_at"`
	AppliedAt       *string        `json:"applied_at"`
	DisplayStatus   string         `json:"display_status"`
	DisplayTone     string         `json:"display_tone"`
}

type RoomIssueLinkResponse struct {
	RoomID          string  `json:"room_id"`
	RoomMessageID   string  `json:"room_message_id"`
	OrchestrationID *string `json:"orchestration_id"`
	IssueID         string  `json:"issue_id"`
	LinkRole        string  `json:"link_role"`
	CreatedAt       string  `json:"created_at"`
}

type SendRoomMessageResponse struct {
	Message       RoomMessageResponse        `json:"message"`
	Orchestration *RoomOrchestrationResponse `json:"orchestration,omitempty"`
	Issues        []IssueResponse            `json:"issues"`
	Links         []RoomIssueLinkResponse    `json:"links"`
	SystemMessage *RoomMessageResponse       `json:"system_message,omitempty"`
}

type createRoomRequest struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Description string  `json:"description"`
	ProjectID   *string `json:"project_id,omitempty"`
}

type updateRoomRequest struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	Description *string `json:"description"`
	Visibility  *string `json:"visibility"`
}

type addRoomMemberRequest struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
}

type createRoomMessageRequest struct {
	ID      *string `json:"id,omitempty"`
	Content string  `json:"content"`
}

var roomMentionRE = regexp.MustCompile(`@([^\s@,，:：\x{4E00}-\x{9FFF}\x{3040}-\x{30FF}]+)`)

func roomToResponse(room db.WorkspaceRoom) RoomResponse {
	return RoomResponse{
		ID:                  uuidToString(room.ID),
		WorkspaceID:         uuidToString(room.WorkspaceID),
		Name:                room.Name,
		DisplayName:         room.DisplayName,
		Description:         room.Description,
		Visibility:          room.Visibility,
		CreatedByID:         uuidToString(room.CreatedByID),
		OrchestratorAgentID: uuidToPtr(room.OrchestratorAgentID),
		ProjectID:           uuidToPtr(room.ProjectID),
		CreatedAt:           timestampToString(room.CreatedAt),
		UpdatedAt:           timestampToString(room.UpdatedAt),
	}
}

// RoomResourceGrantResponse is the JSON shape for a room_resource_grant row.
// agent_name and resource meta are joined in the handler so the UI can render
// the access matrix without a second round-trip.
type RoomResourceGrantResponse struct {
	ID            string `json:"id"`
	RoomID        string `json:"room_id"`
	ResourceID    string `json:"resource_id"`
	AgentID       string `json:"agent_id"`
	AgentName     string `json:"agent_name"`
	ResourceType  string `json:"resource_type"`
	ResourceLabel string `json:"resource_label"`
	AccessLevel   string `json:"access_level"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type updateRoomResourceGrantRequest struct {
	AccessLevel string `json:"access_level"`
}

// fillRoomResourceGrants is the shared backfill helper that materializes the
// "default write" invariant: every agent in a project-based room gets a grant
// for every project resource. It is idempotent (ON CONFLICT DO NOTHING) so
// calling it on room creation, on member add, and on resource add converges
// without clobbering admin-downgraded 'read' grants.
//
// Errors are logged and swallowed: a failure to seed a grant is recoverable
// (the next hook fire or a manual PATCH restores it) and must not fail the
// parent room/member/resource operation that triggered the backfill.
func (h *Handler) fillRoomResourceGrants(ctx context.Context, roomID, projectID pgtype.UUID) {
	resources, err := h.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		slog.Warn("room grant backfill: list project resources failed", "room_id", uuidToString(roomID), "error", err)
		return
	}
	if len(resources) == 0 {
		return
	}
	agentIDs, err := h.Queries.ListRoomAgentMemberIDs(ctx, roomID)
	if err != nil {
		slog.Warn("room grant backfill: list room agents failed", "room_id", uuidToString(roomID), "error", err)
		return
	}
	for _, agentID := range agentIDs {
		for _, res := range resources {
			if err := h.Queries.CreateRoomResourceGrantIfMissing(ctx, db.CreateRoomResourceGrantIfMissingParams{
				RoomID: roomID, ResourceID: res.ID, AgentID: agentID,
			}); err != nil {
				slog.Warn("room grant backfill: insert grant failed", "room_id", uuidToString(roomID), "error", err)
			}
		}
	}
}

// ListRoomResourceGrants returns the access matrix for a project-based room.
// For a plain room (project_id NULL) the result is empty.
func (h *Handler) ListRoomResourceGrants(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	grants, err := h.Queries.ListRoomResourceGrants(r.Context(), room.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list room resource grants")
		return
	}
	// Enrich with agent name + resource meta in bulk to avoid N+1s on the UI.
	agentIDs := make(map[pgtype.UUID]struct{}, len(grants))
	resourceIDs := make(map[pgtype.UUID]struct{}, len(grants))
	for _, g := range grants {
		agentIDs[g.AgentID] = struct{}{}
		resourceIDs[g.ResourceID] = struct{}{}
	}
	agentNames := make(map[string]string, len(agentIDs))
	for id := range agentIDs {
		if ag, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: room.WorkspaceID}); err == nil {
			agentNames[uuidToString(id)] = ag.Name
		}
	}
	resourceMeta := make(map[string]db.ProjectResource, len(resourceIDs))
	for id := range resourceIDs {
		if res, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{ID: id, WorkspaceID: room.WorkspaceID}); err == nil {
			resourceMeta[uuidToString(id)] = res
		}
	}
	resp := make([]RoomResourceGrantResponse, 0, len(grants))
	for _, g := range grants {
		res := resourceMeta[uuidToString(g.ResourceID)]
		label := ""
		if res.Label.Valid {
			label = res.Label.String
		}
		resp = append(resp, RoomResourceGrantResponse{
			ID:            uuidToString(g.ID),
			RoomID:        uuidToString(g.RoomID),
			ResourceID:    uuidToString(g.ResourceID),
			AgentID:       uuidToString(g.AgentID),
			AgentName:     agentNames[uuidToString(g.AgentID)],
			ResourceType:  res.ResourceType,
			ResourceLabel: label,
			AccessLevel:   g.AccessLevel,
			CreatedAt:     timestampToString(g.CreatedAt),
			UpdatedAt:     timestampToString(g.UpdatedAt),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateRoomResourceGrant toggles an agent's access to a resource between
// 'write' and 'read'. Any other access_level value is rejected so a stray
// client string can't land in the DB CHECK constraint as a 500.
func (h *Handler) UpdateRoomResourceGrant(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	if !h.requireWorkspaceAdmin(w, r) {
		return
	}
	grantID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "grantId"), "grant_id")
	if !ok {
		return
	}
	var req updateRoomResourceGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.AccessLevel = strings.TrimSpace(req.AccessLevel)
	if req.AccessLevel != "read" && req.AccessLevel != "write" {
		writeError(w, http.StatusBadRequest, "access_level must be 'read' or 'write'")
		return
	}
	updated, err := h.Queries.UpdateRoomResourceGrantAccess(r.Context(), db.UpdateRoomResourceGrantAccessParams{
		ID:          grantID,
		AccessLevel: req.AccessLevel,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update room resource grant")
		return
	}
	// Enrich the response with agent name + resource meta so the UI can patch
	// its cached row in place without a follow-up fetch.
	agentName := ""
	if ag, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: updated.AgentID, WorkspaceID: room.WorkspaceID}); err == nil {
		agentName = ag.Name
	}
	var resourceType, label string
	if res, err := h.Queries.GetProjectResourceInWorkspace(r.Context(), db.GetProjectResourceInWorkspaceParams{ID: updated.ResourceID, WorkspaceID: room.WorkspaceID}); err == nil {
		resourceType = res.ResourceType
		if res.Label.Valid {
			label = res.Label.String
		}
	}
	resp := RoomResourceGrantResponse{
		ID:            uuidToString(updated.ID),
		RoomID:        uuidToString(updated.RoomID),
		ResourceID:    uuidToString(updated.ResourceID),
		AgentID:       uuidToString(updated.AgentID),
		AgentName:     agentName,
		ResourceType:  resourceType,
		ResourceLabel: label,
		AccessLevel:   updated.AccessLevel,
		CreatedAt:     timestampToString(updated.CreatedAt),
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	}
	writeJSON(w, http.StatusOK, resp)
}

func roomMessageToResponse(msg db.RoomMessage) RoomMessageResponse {
	metadata := map[string]any{}
	if len(msg.Metadata) > 0 {
		_ = json.Unmarshal(msg.Metadata, &metadata)
	}
	return RoomMessageResponse{
		ID:          uuidToString(msg.ID),
		RoomID:      uuidToString(msg.RoomID),
		SenderType:  msg.SenderType,
		SenderID:    uuidToPtr(msg.SenderID),
		MessageType: msg.MessageType,
		Content:     msg.Content,
		Metadata:    metadata,
		CreatedAt:   timestampToString(msg.CreatedAt),
	}
}

func roomOrchestrationToResponse(o db.RoomOrchestration) RoomOrchestrationResponse {
	decision := map[string]any{}
	if len(o.DecisionJson) > 0 {
		_ = json.Unmarshal(o.DecisionJson, &decision)
	}
	return RoomOrchestrationResponse{
		ID:              uuidToString(o.ID),
		RoomID:          uuidToString(o.RoomID),
		SourceMessageID: uuidToString(o.SourceMessageID),
		DecisionSource:  o.DecisionSource,
		DecisionType:    o.DecisionType,
		Status:          o.Status,
		DecisionJSON:    decision,
		Error:           textToPtr(o.Error),
		CreatedAt:       timestampToString(o.CreatedAt),
		AppliedAt:       timestampToPtr(o.AppliedAt),
		DisplayStatus:   fallbackRoomDisplayStatus(o),
		DisplayTone:     fallbackRoomDisplayTone(o),
	}
}

func fallbackRoomDisplayStatus(o db.RoomOrchestration) string {
	if o.Status == "failed" {
		return failedRoomDisplayStatus(o)
	}
	if o.DecisionType == "orchestrator_routing" {
		if o.Status == "applied" {
			return "orchestrator created plan"
		}
		return "orchestrator deciding"
	}
	if o.Status == "applied" {
		switch o.DecisionType {
		case "chat_only":
			return "orchestrator routed to chat"
		case "single_issue":
			return "orchestrator created issue"
		case "issue_dag":
			return "orchestrator created plan"
		case "plan":
			return "orchestrator created plan"
		case "ask_clarification":
			return "orchestrator needs clarification"
		}
	}
	return "orchestrator queued"
}

func fallbackRoomDisplayTone(o db.RoomOrchestration) string {
	if o.Status == "failed" {
		return "failed"
	}
	if o.Status == "applied" {
		return "applied"
	}
	return "pending"
}

func failedRoomDisplayStatus(o db.RoomOrchestration) string {
	if taskfailure.Classify(o.Error.String) == taskfailure.ReasonAgentProviderAuthOrAccess {
		switch o.DecisionSource {
		case "multi_agent_mention":
			return "orchestrator unavailable"
		case "single_agent_mention":
			return "agent unavailable"
		}
	}
	switch o.DecisionSource {
	case "only_issue_command":
		return "issue creation failed"
	case "single_agent_mention":
		return "agent failed"
	case "multi_agent_mention":
		return "orchestrator failed"
	default:
		return "room action failed"
	}
}

func (h *Handler) buildRoomOrchestrationResponses(ctx context.Context, room db.WorkspaceRoom, items []db.RoomOrchestration) []RoomOrchestrationResponse {
	resp := make([]RoomOrchestrationResponse, len(items))
	runtimes, _ := h.Queries.ListAgentRuntimes(ctx, room.WorkspaceID)
	runtimeByID := make(map[string]db.AgentRuntime, len(runtimes))
	for _, rt := range runtimes {
		runtimeByID[uuidToString(rt.ID)] = rt
	}
	for i, item := range items {
		response := roomOrchestrationToResponse(item)
		response.DisplayStatus, response.DisplayTone = h.deriveRoomDisplayStatus(ctx, room, item, runtimeByID)
		resp[i] = response
	}
	return resp
}

func (h *Handler) deriveRoomDisplayStatus(ctx context.Context, room db.WorkspaceRoom, orch db.RoomOrchestration, runtimeByID map[string]db.AgentRuntime) (string, string) {
	if orch.Status == "failed" {
		return failedRoomDisplayStatus(orch), "failed"
	}
	if orch.DecisionType == "orchestrator_routing" {
		if room.OrchestratorAgentID.Valid {
			agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: room.OrchestratorAgentID, WorkspaceID: room.WorkspaceID})
			if err == nil && agent.RuntimeID.Valid {
				if rt, ok := runtimeByID[uuidToString(agent.RuntimeID)]; ok {
					if rt.Status != "online" && orch.Status != "applied" {
						return "orchestrator offline", "warning"
					}
				}
			}
		}
		if orch.Status == "applied" {
			return "orchestrator created plan", "applied"
		}
		return "orchestrator deciding", "pending"
	}
	if orch.Status != "applied" {
		return fallbackRoomDisplayStatus(orch), fallbackRoomDisplayTone(orch)
	}

	if orch.DecisionType == "plan" {
		actions, err := h.Queries.ListRoomOrchestrationActions(ctx, orch.ID)
		if err == nil {
			if status, ok := roomPlanDisplayStatus(actions); ok {
				return status, "applied"
			}
		}
	}

	links, err := h.Queries.ListRoomIssueLinks(ctx, room.ID)
	if err == nil {
		var childStatuses []string
		for _, link := range links {
			if uuidToString(link.OrchestrationID) != uuidToString(orch.ID) {
				continue
			}
			issue, issueErr := h.Queries.GetIssue(ctx, link.IssueID)
			if issueErr != nil || !issue.AssigneeID.Valid {
				continue
			}
			agent, agentErr := h.Queries.GetAgent(ctx, issue.AssigneeID)
			if agentErr != nil {
				continue
			}
			name := strings.TrimSpace(agent.Name)
			if name == "" {
				name = "agent"
			}
			tasks, taskErr := h.Queries.ListActiveTasksByIssue(ctx, issue.ID)
			if taskErr != nil {
				continue
			}
			matched := false
			for _, task := range tasks {
				if uuidToString(task.AgentID) != uuidToString(agent.ID) {
					continue
				}
				matched = true
				switch task.Status {
				case "running", "dispatched":
					childStatuses = append(childStatuses, name+" working")
				case "waiting_local_directory":
					childStatuses = append(childStatuses, name+" waiting for local directory")
				case "queued":
					if agent.RuntimeID.Valid {
						if rt, ok := runtimeByID[uuidToString(agent.RuntimeID)]; ok && rt.Status != "online" {
							childStatuses = append(childStatuses, name+" offline")
							continue
						}
					}
					childStatuses = append(childStatuses, name+" queued")
				}
				break
			}
			if !matched && agent.RuntimeID.Valid {
				if rt, ok := runtimeByID[uuidToString(agent.RuntimeID)]; ok && rt.Status != "online" {
					childStatuses = append(childStatuses, name+" offline")
				}
			}
		}
		if len(childStatuses) > 0 {
			return strings.Join(childStatuses, " | "), "applied"
		}
	}

	switch orch.DecisionType {
	case "chat_only":
		return "orchestrator routed to chat", "applied"
	case "single_issue":
		return "orchestrator created issue", "applied"
	case "issue_dag":
		return "orchestrator created plan", "applied"
	case "plan":
		return "orchestrator created plan", "applied"
	case "ask_clarification":
		return "orchestrator needs clarification", "warning"
	default:
		return fallbackRoomDisplayStatus(orch), fallbackRoomDisplayTone(orch)
	}
}

func roomPlanDisplayStatus(actions []db.RoomOrchestrationAction) (string, bool) {
	if len(actions) == 0 {
		return "", false
	}
	counts := map[string]int{}
	for _, action := range actions {
		counts[action.Status]++
	}
	switch {
	case counts["failed"] > 0:
		return "room plan has failed actions", true
	case counts["running"] > 0:
		return fmt.Sprintf("room plan running (%d active)", counts["running"]), true
	case counts["blocked"] > 0:
		return fmt.Sprintf("room plan waiting (%d blocked)", counts["blocked"]), true
	case counts["done"] == len(actions):
		return "room plan completed", true
	default:
		return "orchestrator created plan", true
	}
}

func roomIssueLinkToResponse(link db.RoomIssueLink) RoomIssueLinkResponse {
	return RoomIssueLinkResponse{
		RoomID:          uuidToString(link.RoomID),
		RoomMessageID:   uuidToString(link.RoomMessageID),
		OrchestrationID: uuidToPtr(link.OrchestrationID),
		IssueID:         uuidToString(link.IssueID),
		LinkRole:        link.LinkRole,
		CreatedAt:       timestampToString(link.CreatedAt),
	}
}

func roomDisplayName(name, displayName string) (string, string) {
	displayName = strings.TrimSpace(displayName)
	name = strings.TrimSpace(name)
	if displayName == "" {
		displayName = name
	}
	if name == "" {
		name = displayName
	}
	name = normalizeRoomName(name)
	if displayName == "" {
		displayName = name
	}
	return name, displayName
}

func normalizeRoomName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "#"))
	var out []rune
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out = append(out, r)
			lastDash = false
		case !lastDash:
			out = append(out, '-')
			lastDash = true
		}
	}
	normalized := strings.Trim(string(out), "-")
	if normalized == "" {
		return "room"
	}
	return normalized
}

func (h *Handler) ListRooms(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.workspaceUUIDFromContext(w, r)
	if !ok {
		return
	}
	rooms, err := h.Queries.ListRooms(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list rooms")
		return
	}
	resp := make([]RoomResponse, len(rooms))
	for i, room := range rooms {
		resp[i] = roomToResponse(room)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.workspaceUUIDFromContext(w, r)
	if !ok {
		return
	}
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "workspace member required")
		return
	}
	var req createRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// If project_id is supplied, resolve and ownership-check the project here so
	// a bad/foreign project ID fails before we create the room. The partial
	// unique index on workspace_room.project_id makes a duplicate project-based
	// room a 409 rather than a silent second room.
	var projectID pgtype.UUID
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		resolved, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: resolved, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		if _, err := h.Queries.GetRoomByProjectID(r.Context(), project.ID); err == nil {
			writeError(w, http.StatusConflict, "this project already has a room")
			return
		} else if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to check existing project room")
			return
		}
		projectID = project.ID
	}

	name, displayName := roomDisplayName(req.Name, req.DisplayName)
	room, err := h.Queries.CreateRoom(r.Context(), db.CreateRoomParams{
		WorkspaceID: wsUUID,
		Name:        name,
		DisplayName: displayName,
		Description: strings.TrimSpace(req.Description),
		Visibility:  "workspace",
		CreatedByID: member.ID,
		ProjectID:   projectID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			// Either the room name collided or another project-based room for the
			// same project raced ahead of us — the GET-then-INSERT sequence above
			// is not atomic. Surface a conflict either way.
			writeError(w, http.StatusConflict, "room name or project binding already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create room")
		return
	}
	_, _ = h.Queries.AddRoomMember(r.Context(), db.AddRoomMemberParams{
		RoomID:     room.ID,
		MemberType: "member",
		MemberID:   member.UserID,
		JoinedByID: member.ID,
	})
	room = h.maybeCreateRoomOrchestrator(r.Context(), room, member)
	// For a project-based room, seed default 'write' grants for every agent
	// currently in the room (the orchestrator, at minimum) against every
	// project resource. Executor agents added later get their grants via the
	// AddRoomMember hook; resources added later via CreateProjectResource.
	if room.ProjectID.Valid {
		h.fillRoomResourceGrants(r.Context(), room.ID, room.ProjectID)
	}
	writeJSON(w, http.StatusCreated, roomToResponse(room))
}

func (h *Handler) GetRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, roomToResponse(room))
}

func (h *Handler) UpdateRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	if !h.requireWorkspaceAdmin(w, r) {
		return
	}
	var req updateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Visibility != nil && *req.Visibility != "workspace" {
		writeError(w, http.StatusBadRequest, "private rooms are not supported yet")
		return
	}
	var name pgtype.Text
	var displayName pgtype.Text
	if req.Name != nil || req.DisplayName != nil {
		currentName := room.Name
		currentDisplay := room.DisplayName
		if req.Name != nil {
			currentName = *req.Name
		}
		if req.DisplayName != nil {
			currentDisplay = *req.DisplayName
		}
		n, d := roomDisplayName(currentName, currentDisplay)
		name = pgtype.Text{String: n, Valid: true}
		displayName = pgtype.Text{String: d, Valid: true}
	}
	updated, err := h.Queries.UpdateRoom(r.Context(), db.UpdateRoomParams{
		ID:          room.ID,
		WorkspaceID: room.WorkspaceID,
		Name:        name,
		DisplayName: displayName,
		Description: ptrToText(req.Description),
		Visibility:  ptrToText(req.Visibility),
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "room name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update room")
		return
	}
	writeJSON(w, http.StatusOK, roomToResponse(updated))
}

func (h *Handler) DeleteRoom(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	if !h.requireWorkspaceAdmin(w, r) {
		return
	}
	if err := h.Queries.DeleteRoom(r.Context(), db.DeleteRoomParams{ID: room.ID, WorkspaceID: room.WorkspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete room")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListRoomMembers(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	members, err := h.Queries.ListRoomMembers(r.Context(), room.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list room members")
		return
	}
	resp := make([]RoomMemberResponse, 0, len(members))
	for _, m := range members {
		item := RoomMemberResponse{
			RoomID:     uuidToString(m.RoomID),
			MemberType: m.MemberType,
			MemberID:   uuidToString(m.MemberID),
			JoinedByID: uuidToPtr(m.JoinedByID),
			JoinedAt:   timestampToString(m.JoinedAt),
		}
		switch m.MemberType {
		case "agent":
			if agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: m.MemberID, WorkspaceID: room.WorkspaceID}); err == nil {
				item.Name = agent.Name
				item.AvatarURL = textToPtr(agent.AvatarUrl)
			}
		case "member":
			if member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: m.MemberID, WorkspaceID: room.WorkspaceID}); err == nil {
				if user, err := h.Queries.GetUser(r.Context(), member.UserID); err == nil {
					item.Name = user.Name
					item.AvatarURL = textToPtr(user.AvatarUrl)
				}
			}
		}
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) AddRoomMember(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	if !h.requireWorkspaceAdmin(w, r) {
		return
	}
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "workspace member required")
		return
	}
	var req addRoomMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, req.MemberID, "member_id")
	if !ok {
		return
	}
	switch req.MemberType {
	case "member":
		if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: targetID, WorkspaceID: room.WorkspaceID}); err != nil {
			writeError(w, http.StatusBadRequest, "member_id does not refer to a member of this workspace")
			return
		}
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: targetID, WorkspaceID: room.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "member_id does not refer to an active agent of this workspace")
			return
		}
		if _, err := h.Queries.GetRoomByOrchestratorAgent(r.Context(), db.GetRoomByOrchestratorAgentParams{
			WorkspaceID:         room.WorkspaceID,
			OrchestratorAgentID: targetID,
		}); err == nil {
			writeError(w, http.StatusBadRequest, "room orchestrator agents are bound to their own room")
			return
		} else if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to validate agent")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "member_type must be 'member' or 'agent'")
		return
	}
	added, err := h.Queries.AddRoomMember(r.Context(), db.AddRoomMemberParams{
		RoomID:     room.ID,
		MemberType: req.MemberType,
		MemberID:   targetID,
		JoinedByID: member.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add room member")
		return
	}
	// For a project-based room, a newly-joined agent gets default 'write'
	// grants for every project resource. The helper is idempotent so re-adding
	// a member is a no-op on existing grants.
	if req.MemberType == "agent" && room.ProjectID.Valid {
		h.fillRoomResourceGrants(r.Context(), room.ID, room.ProjectID)
	}
	writeJSON(w, http.StatusCreated, RoomMemberResponse{
		RoomID:     uuidToString(added.RoomID),
		MemberType: added.MemberType,
		MemberID:   uuidToString(added.MemberID),
		JoinedByID: uuidToPtr(added.JoinedByID),
		JoinedAt:   timestampToString(added.JoinedAt),
	})
}

func (h *Handler) DeleteRoomMember(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	if !h.requireWorkspaceAdmin(w, r) {
		return
	}
	memberType := chi.URLParam(r, "memberType")
	memberID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "memberId"), "member_id")
	if !ok {
		return
	}
	if memberType != "member" && memberType != "agent" {
		writeError(w, http.StatusBadRequest, "member_type must be 'member' or 'agent'")
		return
	}
	if err := h.Queries.DeleteRoomMember(r.Context(), db.DeleteRoomMemberParams{RoomID: room.ID, MemberType: memberType, MemberID: memberID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete room member")
		return
	}
	// Cascade: drop the agent's resource grants in this room so stale access
	// rows don't survive a member removal. No-op for member-type removals.
	if memberType == "agent" {
		if err := h.Queries.DeleteRoomResourceGrantsForMember(r.Context(), db.DeleteRoomResourceGrantsForMemberParams{RoomID: room.ID, AgentID: memberID}); err != nil {
			slog.Warn("failed to delete room resource grants for removed agent", "room_id", uuidToString(room.ID), "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListRoomMessages(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	messages, err := h.Queries.ListRoomMessages(r.Context(), db.ListRoomMessagesParams{RoomID: room.ID, Limit: 200})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list room messages")
		return
	}
	resp := make([]RoomMessageResponse, len(messages))
	for i, msg := range messages {
		resp[i] = roomMessageToResponse(msg)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListRoomOrchestrations(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	items, err := h.Queries.ListRoomOrchestrations(r.Context(), room.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list room orchestrations")
		return
	}
	resp := h.buildRoomOrchestrationResponses(r.Context(), room, items)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateRoomMessage(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	var req createRoomMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "workspace member required")
		return
	}
	var clientID pgtype.UUID
	if req.ID != nil && *req.ID != "" {
		id, ok := parseUUIDOrBadRequest(w, *req.ID, "id")
		if !ok {
			return
		}
		clientID = id
	}
	msg, err := h.Queries.CreateRoomMessage(r.Context(), db.CreateRoomMessageParams{
		RoomID:      room.ID,
		SenderType:  "member",
		SenderID:    member.UserID,
		MessageType: "human",
		Content:     content,
		Metadata:    []byte(`{}`),
		ID:          clientID,
	})
	if err != nil {
		if isUniqueViolation(err) && req.ID != nil {
			id, ok := parseUUIDOrBadRequest(w, *req.ID, "id")
			if !ok {
				return
			}
			existing, getErr := h.Queries.GetRoomMessageInRoom(r.Context(), db.GetRoomMessageInRoomParams{ID: id, RoomID: room.ID})
			if getErr != nil {
				writeError(w, http.StatusConflict, "message id already exists")
				return
			}
			h.writeRoomMessageResult(w, r, room, existing, http.StatusOK)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create room message")
		return
	}
	h.writeRoomMessageResult(w, r, room, msg, http.StatusCreated)
}

type RoomIssueResponse struct {
	Link  RoomIssueLinkResponse `json:"link"`
	Issue *IssueResponse        `json:"issue,omitempty"`
}

func (h *Handler) ListRoomIssues(w http.ResponseWriter, r *http.Request) {
	room, ok := h.loadRoomForUser(w, r)
	if !ok {
		return
	}
	links, err := h.Queries.ListRoomIssueLinks(r.Context(), room.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list room issue links")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), room.WorkspaceID)
	resp := make([]RoomIssueResponse, 0, len(links))
	for _, link := range links {
		item := RoomIssueResponse{Link: roomIssueLinkToResponse(link)}
		if issue, err := h.Queries.GetIssue(r.Context(), link.IssueID); err == nil {
			ir := issueToResponse(issue, prefix)
			item.Issue = &ir
		}
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeRoomMessageResult(w http.ResponseWriter, r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, status int) {
	existing, err := h.Queries.GetRoomOrchestrationBySourceMessage(r.Context(), msg.ID)
	if err == nil {
		links, _ := h.Queries.ListRoomIssueLinks(r.Context(), room.ID)
		messageLinks := dbRoomIssueLinksForMessage(links, msg.ID)
		orchResp := h.buildRoomOrchestrationResponses(r.Context(), room, []db.RoomOrchestration{existing})
		var orchPtr *RoomOrchestrationResponse
		if len(orchResp) > 0 {
			orchPtr = &orchResp[0]
		}
		resp := SendRoomMessageResponse{
			Message:       roomMessageToResponse(msg),
			Orchestration: orchPtr,
			Issues:        h.issuesForLinks(r, messageLinks),
			Links:         roomIssueLinksToResponse(messageLinks),
		}
		writeJSON(w, status, resp)
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load room orchestration")
		return
	}
	resp, err := h.applyRoomMessage(r, room, msg)
	if err != nil {
		slog.Warn("room message orchestration failed", append(logger.RequestAttrs(r), "error", err, "room_id", uuidToString(room.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to process room message")
		return
	}
	writeJSON(w, status, resp)
}

func ptrRoomOrchestrationWithRoom(h *Handler, ctx context.Context, room db.WorkspaceRoom, o db.RoomOrchestration) *RoomOrchestrationResponse {
	respList := h.buildRoomOrchestrationResponses(ctx, room, []db.RoomOrchestration{o})
	if len(respList) == 0 {
		return nil
	}
	resp := respList[0]
	return &resp
}

func dbRoomIssueLinksForMessage(links []db.RoomIssueLink, messageID pgtype.UUID) []db.RoomIssueLink {
	out := []db.RoomIssueLink{}
	for _, link := range links {
		if uuidToString(link.RoomMessageID) == uuidToString(messageID) {
			out = append(out, link)
		}
	}
	return out
}

func roomIssueLinksToResponse(links []db.RoomIssueLink) []RoomIssueLinkResponse {
	out := make([]RoomIssueLinkResponse, len(links))
	for i, link := range links {
		out[i] = roomIssueLinkToResponse(link)
	}
	return out
}

func (h *Handler) applyRoomMessage(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage) (SendRoomMessageResponse, error) {
	content := strings.TrimSpace(msg.Content)
	resp := SendRoomMessageResponse{Message: roomMessageToResponse(msg), Issues: []IssueResponse{}, Links: []RoomIssueLinkResponse{}}
	mentionedAgents, err := h.mentionedRoomAgents(r, room, content)
	if err != nil {
		return resp, err
	}
	isIssueCommand := strings.HasPrefix(content, "/issue")

	switch {
	case len(mentionedAgents) == 0 && isIssueCommand:
		return h.applyRoomIssueCommand(r, room, msg, content, resp)
	case len(mentionedAgents) == 1:
		return h.applyRoomSingleAgentMention(r, room, msg, content, mentionedAgents[0], resp)
	case len(mentionedAgents) >= 2:
		return h.applyRoomMultiAgentMention(r, room, msg, content, mentionedAgents, isIssueCommand, resp)
	}
	return resp, nil
}

func (h *Handler) applyRoomIssueCommand(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, content string, resp SendRoomMessageResponse) (SendRoomMessageResponse, error) {
	title := strings.TrimSpace(strings.TrimPrefix(content, "/issue"))
	if title == "" {
		title = "Untitled issue"
	}
	orch, issue, link, err := h.createRoomSingleIssue(r, room, msg, "only_issue_command", title, content, pgtype.UUID{}, "backlog")
	if err != nil {
		return resp, err
	}
	resp.Orchestration = ptrRoomOrchestrationWithRoom(h, r.Context(), room, orch)
	resp.Issues = []IssueResponse{issue}
	resp.Links = []RoomIssueLinkResponse{link}
	return resp, nil
}

func (h *Handler) applyRoomSingleAgentMention(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, content string, agent db.Agent, resp SendRoomMessageResponse) (SendRoomMessageResponse, error) {
	orch, err := h.createRoomChatOnly(r, room, msg, content, agent)
	if err != nil {
		return resp, err
	}
	resp.Orchestration = ptrRoomOrchestrationWithRoom(h, r.Context(), room, orch)
	return resp, nil
}

func (h *Handler) applyRoomMultiAgentMention(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, content string, mentionedAgents []db.Agent, forceIssueIntent bool, resp SendRoomMessageResponse) (SendRoomMessageResponse, error) {
	if !room.OrchestratorAgentID.Valid {
		return h.applyRoomMultiAgentChatFallback(r, room, msg, content, mentionedAgents, resp)
	}
	orchestratorAgent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          room.OrchestratorAgentID,
		WorkspaceID: room.WorkspaceID,
	})
	if err != nil || orchestratorAgent.ArchivedAt.Valid {
		return h.applyRoomMultiAgentChatFallback(r, room, msg, content, mentionedAgents, resp)
	}
	if !h.roomOrchestratorReady(orchestratorAgent) {
		return h.applyRoomMultiAgentChatFallback(r, room, msg, content, mentionedAgents, resp)
	}
	orch, err := h.routeToRoomOrchestrator(r, room, msg, content, mentionedAgents, orchestratorAgent, forceIssueIntent)
	if err != nil {
		return resp, err
	}
	resp.Orchestration = ptrRoomOrchestrationWithRoom(h, r.Context(), room, orch)
	return resp, nil
}

func (h *Handler) roomOrchestratorReady(agent db.Agent) bool {
	if !agent.RuntimeID.Valid {
		return false
	}
	rt, err := h.Queries.GetAgentRuntime(context.Background(), agent.RuntimeID)
	if err != nil {
		return false
	}
	return rt.Status == "online" && rt.Provider == "codex"
}

func (h *Handler) applyRoomMultiAgentChatFallback(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, content string, mentionedAgents []db.Agent, resp SendRoomMessageResponse) (SendRoomMessageResponse, error) {
	orchs := make([]db.RoomOrchestration, 0, len(mentionedAgents))
	for _, agent := range mentionedAgents {
		orch, err := h.createRoomChatOnlyWithType(r, room, msg, content, agent, "multi_agent_mention", "chat_only")
		if err != nil {
			return resp, err
		}
		orchs = append(orchs, orch)
	}
	if len(orchs) > 0 {
		resp.Orchestration = ptrRoomOrchestrationWithRoom(h, r.Context(), room, orchs[0])
	}
	return resp, nil
}

func (h *Handler) applyRoomMissingOrchestrator(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, resp SendRoomMessageResponse) (SendRoomMessageResponse, error) {
	orch, systemMsg, err := h.createRoomClarification(r, room, msg, "Room orchestrator is not available. Try again after an online runtime is connected, or mention one agent at a time.")
	if err != nil {
		return resp, err
	}
	resp.Orchestration = ptrRoomOrchestrationWithRoom(h, r.Context(), room, orch)
	systemResp := roomMessageToResponse(systemMsg)
	resp.SystemMessage = &systemResp
	return resp, nil
}

func (h *Handler) createRoomSingleIssue(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, source, title, input string, agentID pgtype.UUID, status string) (db.RoomOrchestration, IssueResponse, RoomIssueLinkResponse, error) {
	inputSnapshot, _ := json.Marshal(map[string]any{"content": input})
	decision, _ := json.Marshal(map[string]any{"title": title, "assignee_type": nullableAssigneeType(agentID), "assignee_id": uuidToPtr(agentID)})
	orch, err := h.Queries.CreateRoomOrchestration(r.Context(), db.CreateRoomOrchestrationParams{
		RoomID:          room.ID,
		SourceMessageID: msg.ID,
		DecisionSource:  source,
		DecisionType:    "single_issue",
		Status:          "pending",
		InputSnapshot:   inputSnapshot,
		DecisionJson:    decision,
	})
	if err != nil {
		return db.RoomOrchestration{}, IssueResponse{}, RoomIssueLinkResponse{}, err
	}
	var assigneeType pgtype.Text
	if agentID.Valid {
		assigneeType = pgtype.Text{String: "agent", Valid: true}
	}
	creatorID := requestUserID(r)
	if status == "" {
		status = "todo"
	}
	res, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID:    room.WorkspaceID,
		Title:          title,
		Description:    pgtype.Text{String: input, Valid: input != ""},
		Status:         status,
		Priority:       "none",
		AssigneeType:   assigneeType,
		AssigneeID:     agentID,
		CreatorType:    "member",
		CreatorID:      parseUUID(creatorID),
		OriginType:     pgtype.Text{String: "room_message", Valid: true},
		OriginID:       msg.ID,
		AllowDuplicate: false,
	}, service.IssueCreateOpts{
		ActorID: creatorID,
		Platform: func() string {
			p, _, _ := middleware.ClientMetadataFromContext(r.Context())
			return p
		}(),
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment) map[string]any {
			return map[string]any{"issue": issueToResponse(issue, h.getIssuePrefix(r.Context(), room.WorkspaceID))}
		},
	})
	if err != nil {
		if errors.Is(err, service.ErrActiveDuplicate) && res.DuplicateIssue != nil {
			// Silently reuse the existing issue — the user sent the same task twice.
			orch, _ = h.Queries.UpdateRoomOrchestrationApplied(r.Context(), db.UpdateRoomOrchestrationAppliedParams{ID: orch.ID, DecisionJson: decision})
			link, _ := h.Queries.CreateRoomIssueLink(r.Context(), db.CreateRoomIssueLinkParams{
				RoomID:          room.ID,
				RoomMessageID:   msg.ID,
				OrchestrationID: orch.ID,
				IssueID:         res.DuplicateIssue.ID,
				LinkRole:        "related",
			})
			return orch, issueToResponse(*res.DuplicateIssue, h.getIssuePrefix(r.Context(), room.WorkspaceID)), roomIssueLinkToResponse(link), nil
		}
		_, _ = h.Queries.UpdateRoomOrchestrationFailed(r.Context(), db.UpdateRoomOrchestrationFailedParams{ID: orch.ID, Error: pgtype.Text{String: err.Error(), Valid: true}})
		return db.RoomOrchestration{}, IssueResponse{}, RoomIssueLinkResponse{}, err
	}
	orch, err = h.Queries.UpdateRoomOrchestrationApplied(r.Context(), db.UpdateRoomOrchestrationAppliedParams{ID: orch.ID, DecisionJson: decision})
	if err != nil {
		return db.RoomOrchestration{}, IssueResponse{}, RoomIssueLinkResponse{}, err
	}
	link, err := h.Queries.CreateRoomIssueLink(r.Context(), db.CreateRoomIssueLinkParams{
		RoomID:          room.ID,
		RoomMessageID:   msg.ID,
		OrchestrationID: orch.ID,
		IssueID:         res.Issue.ID,
		LinkRole:        "root",
	})
	if err != nil {
		return db.RoomOrchestration{}, IssueResponse{}, RoomIssueLinkResponse{}, err
	}
	return orch, issueToResponse(res.Issue, h.getIssuePrefix(r.Context(), room.WorkspaceID)), roomIssueLinkToResponse(link), nil
}

func (h *Handler) createRoomChatOnly(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, input string, agent db.Agent) (db.RoomOrchestration, error) {
	return h.createRoomChatOnlyWithType(r, room, msg, input, agent, "single_agent_mention", "chat_only")
}

func (h *Handler) createRoomChatOnlyWithType(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, input string, agent db.Agent, decisionSource string, decisionType string) (db.RoomOrchestration, error) {
	userID := requestUserID(r)
	userUUID := parseUUID(userID)

	// Build a sender-name map from room members so the context is human-readable.
	memberNameMap := h.buildRoomSenderNames(r, room)

	// Fetch the 20 most recent messages (DESC), then reverse to chronological order.
	recentDesc, _ := h.Queries.ListRecentRoomMessages(r.Context(), db.ListRecentRoomMessagesParams{
		RoomID: room.ID,
		Limit:  21, // 21 so we can drop the current msg if it's already in the list
	})
	// Build context excluding the current message and system messages.
	var contextLines []string
	for i := len(recentDesc) - 1; i >= 0; i-- {
		m := recentDesc[i]
		if uuidToString(m.ID) == uuidToString(msg.ID) || m.SenderType == "system" {
			continue
		}
		name := memberNameMap[uuidToString(m.SenderID)]
		if name == "" {
			name = m.SenderType
		}
		contextLines = append(contextLines, name+": "+m.Content)
	}

	// Build final user message: context header + current message.
	userContent := input
	if len(contextLines) > 0 {
		userContent = "[Recent room chat]\n" + strings.Join(contextLines, "\n") + "\n\n" + input
	}

	// Create a chat session between the user and the mentioned agent.
	session, err := h.Queries.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: room.WorkspaceID,
		AgentID:     agent.ID,
		CreatorID:   userUUID,
		Title:       input,
	})
	if err != nil {
		return db.RoomOrchestration{}, err
	}

	// Write the enriched user message (with room context) into the chat session.
	chatMsg, err := h.Queries.CreateChatMessage(r.Context(), db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       userContent,
	})
	if err != nil {
		return db.RoomOrchestration{}, err
	}

	// Enqueue the chat task so the agent processes it.
	task, err := h.TaskService.EnqueueChatTask(r.Context(), session, userUUID, false)
	if err != nil {
		return db.RoomOrchestration{}, err
	}
	// Link the user chat message to the task so cancel/cleanup works correctly.
	_ = h.Queries.LinkChatMessageToTask(r.Context(), db.LinkChatMessageToTaskParams{
		ID:     chatMsg.ID,
		TaskID: task.ID,
	})

	inputSnapshot, _ := json.Marshal(map[string]any{"content": input})
	decision, _ := json.Marshal(map[string]any{
		"decision_type":   decisionType,
		"chat_session_id": uuidToString(session.ID),
		"agent_id":        uuidToString(agent.ID),
	})
	status := "applied"
	appliedAt := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	if decisionType == "orchestrator_routing" {
		// Enqueueing the orchestrator chat is not the same as applying a
		// routing decision. Keep the orchestration pending until the reply's
		// <orchestrator-decision> block is parsed and persisted.
		status = "pending"
		appliedAt = pgtype.Timestamptz{}
	}
	orch, err := h.Queries.CreateRoomOrchestration(r.Context(), db.CreateRoomOrchestrationParams{
		RoomID:          room.ID,
		SourceMessageID: msg.ID,
		DecisionSource:  decisionSource,
		DecisionType:    decisionType,
		Status:          status,
		InputSnapshot:   inputSnapshot,
		DecisionJson:    decision,
		ChatSessionID:   session.ID,
		AppliedAt:       appliedAt,
	})
	return orch, err
}

func (h *Handler) createRoomClarification(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, text string) (db.RoomOrchestration, db.RoomMessage, error) {
	inputSnapshot, _ := json.Marshal(map[string]any{"content": msg.Content})
	decision, _ := json.Marshal(map[string]any{"message": text})
	orch, err := h.Queries.CreateRoomOrchestration(r.Context(), db.CreateRoomOrchestrationParams{
		RoomID:          room.ID,
		SourceMessageID: msg.ID,
		DecisionSource:  "multi_agent_mention",
		DecisionType:    "ask_clarification",
		Status:          "applied",
		InputSnapshot:   inputSnapshot,
		DecisionJson:    decision,
		AppliedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return db.RoomOrchestration{}, db.RoomMessage{}, err
	}
	systemMsg, err := h.Queries.CreateRoomMessage(r.Context(), db.CreateRoomMessageParams{
		RoomID:      room.ID,
		SenderType:  "system",
		MessageType: "system",
		Content:     text,
		Metadata:    []byte(`{"kind":"ask_clarification"}`),
	})
	return orch, systemMsg, err
}

func (h *Handler) mentionedRoomAgents(r *http.Request, room db.WorkspaceRoom, content string) ([]db.Agent, error) {
	agents, err := h.roomAgents(r, room)
	if err != nil {
		return nil, err
	}
	mentions := roomMentionRE.FindAllStringSubmatch(content, -1)
	out := []db.Agent{}
	seen := map[string]bool{}
	for _, match := range mentions {
		if len(match) < 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(match[1]))
		for _, agent := range agents {
			if strings.ToLower(agent.Name) == name || strings.ToLower(normalizeRoomName(agent.Name)) == name {
				id := uuidToString(agent.ID)
				if !seen[id] {
					out = append(out, agent)
					seen[id] = true
				}
			}
		}
	}
	return out, nil
}

func (h *Handler) roomAgents(r *http.Request, room db.WorkspaceRoom) ([]db.Agent, error) {
	members, err := h.Queries.ListRoomMembers(r.Context(), room.ID)
	if err != nil {
		return nil, err
	}
	agents := []db.Agent{}
	for _, member := range members {
		if member.MemberType != "agent" {
			continue
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: member.MemberID, WorkspaceID: room.WorkspaceID})
		if err == nil && !agent.ArchivedAt.Valid {
			agents = append(agents, agent)
		}
	}
	return agents, nil
}

// buildRoomSenderNames returns a map of sender UUID → display name for all
// current room members (both agents and human members).
func (h *Handler) buildRoomSenderNames(r *http.Request, room db.WorkspaceRoom) map[string]string {
	out := make(map[string]string)
	members, err := h.Queries.ListRoomMembers(r.Context(), room.ID)
	if err != nil {
		return out
	}
	for _, m := range members {
		id := uuidToString(m.MemberID)
		switch m.MemberType {
		case "agent":
			if agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: m.MemberID, WorkspaceID: room.WorkspaceID}); err == nil {
				out[id] = agent.Name
			}
		case "member":
			if member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: m.MemberID, WorkspaceID: room.WorkspaceID}); err == nil {
				if user, err := h.Queries.GetUser(r.Context(), member.UserID); err == nil {
					out[id] = user.Name
				}
			}
		}
	}
	return out
}

func nullableAssigneeType(agentID pgtype.UUID) *string {
	if !agentID.Valid {
		return nil
	}
	v := "agent"
	return &v
}

// roomOrchestratorInstructions is the system prompt stored in the orchestrator
// agent's instructions field. It activates the orchestrator role and references
// the multica-room-orchestrating builtin skill for output format details.
const roomOrchestratorInstructions = `You are the built-in Room Orchestrator for room #%s.

When multiple agents are @mentioned in a room message, that message is routed to you. Your job is to analyze the request and decide how to coordinate the work.

Always end your reply with an <orchestrator-decision> block. Follow the multica-room-orchestrating skill for the exact JSON format and stage rules.

Quick reference:
- Return {"type":"plan","actions":[...]}.
- action.mode="chat" means the agent should answer in room chat.
- action.mode="issue" means create a tracked issue for the agent.
- Use depends_on to fan-in later actions after earlier chat or issue outputs.
- Put parallel actions in the same stage; dependent actions use later stages.

Use the exact agent names as they appear in the @mentions.`

func pickRoomOrchestratorRuntime(runtimes []db.AgentRuntime) (db.AgentRuntime, bool) {
	for _, runtime := range runtimes {
		if runtime.Status == "online" && runtime.Provider == "codex" {
			return runtime, true
		}
	}
	for _, runtime := range runtimes {
		if runtime.Status == "online" {
			return runtime, true
		}
	}
	if len(runtimes) == 0 {
		return db.AgentRuntime{}, false
	}
	return runtimes[0], true
}

// maybeCreateRoomOrchestrator auto-creates a Room Orchestrator agent for the
// room using the preferred available workspace runtime. Soft-fails silently if
// no runtime exists. Returns the updated room (with OrchestratorAgentID set) on
// success, or the original room on failure.
func (h *Handler) maybeCreateRoomOrchestrator(ctx context.Context, room db.WorkspaceRoom, member db.Member) db.WorkspaceRoom {
	runtimes, err := h.Queries.ListAgentRuntimes(ctx, room.WorkspaceID)
	if err != nil || len(runtimes) == 0 {
		return room
	}
	runtime, ok := pickRoomOrchestratorRuntime(runtimes)
	if !ok {
		return room
	}

	agentName := "#" + room.Name + " Orchestrator"
	instructions := fmt.Sprintf(roomOrchestratorInstructions, room.Name)
	agent, err := h.Queries.CreateAgent(ctx, db.CreateAgentParams{
		WorkspaceID:        room.WorkspaceID,
		Name:               agentName,
		Description:        "Built-in room orchestrator for #" + room.Name + ". Coordinates multi-agent tasks.",
		Instructions:       instructions,
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeConfig:      []byte("{}"),
		RuntimeID:          runtime.ID,
		Visibility:         "workspace",
		MaxConcurrentTasks: 1,
		OwnerID:            member.UserID,
		CustomEnv:          []byte("{}"),
		CustomArgs:         []byte("[]"),
	})
	if err != nil {
		slog.Warn("failed to create room orchestrator agent", "room_id", uuidToString(room.ID), "error", err)
		return room
	}

	_, _ = h.Queries.AddRoomMember(ctx, db.AddRoomMemberParams{
		RoomID:     room.ID,
		MemberType: "agent",
		MemberID:   agent.ID,
		JoinedByID: member.ID,
	})

	updated, err := h.Queries.UpdateRoomOrchestratorAgent(ctx, db.UpdateRoomOrchestratorAgentParams{
		ID:                  room.ID,
		OrchestratorAgentID: agent.ID,
	})
	if err != nil {
		slog.Warn("failed to set room orchestrator_agent_id", "room_id", uuidToString(room.ID), "error", err)
		return room
	}
	return updated
}

// routeToRoomOrchestrator sends a multi-agent message to the room's orchestrator
// agent so the LLM can decide the execution structure (chat vs single_issue vs
// issue_dag). The orchestrator's reply is processed by maybeWriteAgentReplyToRoom
// in TaskService, which fires the RoomOrchestratorApply callback.
func (h *Handler) routeToRoomOrchestrator(r *http.Request, room db.WorkspaceRoom, msg db.RoomMessage, content string, mentionedAgents []db.Agent, orchestratorAgent db.Agent, forceIssueIntent bool) (db.RoomOrchestration, error) {
	memberNameMap := h.buildRoomSenderNames(r, room)
	recentDesc, _ := h.Queries.ListRecentRoomMessages(r.Context(), db.ListRecentRoomMessagesParams{
		RoomID: room.ID,
		Limit:  21,
	})
	var contextLines []string
	for i := len(recentDesc) - 1; i >= 0; i-- {
		m := recentDesc[i]
		if uuidToString(m.ID) == uuidToString(msg.ID) || m.SenderType == "system" {
			continue
		}
		name := memberNameMap[uuidToString(m.SenderID)]
		if name == "" {
			name = m.SenderType
		}
		contextLines = append(contextLines, name+": "+m.Content)
	}

	agentNames := make([]string, len(mentionedAgents))
	for i, a := range mentionedAgents {
		agentNames[i] = a.Name
	}

	var orchestratorInput strings.Builder
	orchestratorInput.WriteString("[Coordination request]\n")
	orchestratorInput.WriteString("Agents mentioned: " + strings.Join(agentNames, ", ") + "\n")
	if forceIssueIntent {
		orchestratorInput.WriteString("Issue intent: the message starts with /issue, so prefer single_issue or issue_dag unless the request is clearly only conversational.\n")
	}
	if len(contextLines) > 0 {
		orchestratorInput.WriteString("\n[Recent room chat]\n")
		orchestratorInput.WriteString(strings.Join(contextLines, "\n"))
		orchestratorInput.WriteString("\n")
	}
	orchestratorInput.WriteString("\nMessage: " + content)

	return h.createRoomChatOnlyWithType(r, room, msg, orchestratorInput.String(), orchestratorAgent, "multi_agent_mention", "orchestrator_routing")
}

// buildRoomAgentNameMapCtx returns a map of lowercased agent name → agent UUID
// for all active agent members in the room. Used by applyOrchestratorDecision
// to resolve agent names from the orchestrator's JSON decision.
func (h *Handler) buildRoomAgentNameMapCtx(ctx context.Context, room db.WorkspaceRoom) (map[string]pgtype.UUID, error) {
	members, err := h.Queries.ListRoomMembers(ctx, room.ID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]pgtype.UUID)
	for _, m := range members {
		if m.MemberType != "agent" {
			continue
		}
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          m.MemberID,
			WorkspaceID: room.WorkspaceID,
		})
		if err == nil && !agent.ArchivedAt.Valid {
			out[strings.ToLower(agent.Name)] = agent.ID
			out[strings.ToLower(normalizeRoomName(agent.Name))] = agent.ID
		}
	}
	return out, nil
}

type roomPlanDecision struct {
	Type    string           `json:"type"`
	Title   string           `json:"title"`
	Agent   string           `json:"agent"`
	Actions []roomPlanAction `json:"actions"`
	Nodes   []roomPlanAction `json:"nodes"`
	Edges   []struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"edges"`
}

type roomPlanAction struct {
	Key        string   `json:"key"`
	Mode       string   `json:"mode"`
	Agent      string   `json:"agent"`
	Title      string   `json:"title"`
	Stage      int32    `json:"stage"`
	Deliverable string  `json:"deliverable"`
	DependsOn  []string `json:"depends_on"`
}

// ApplyOrchestratorDecision parses the orchestrator decision, normalizes both
// the new plan/actions shape and older room decision shapes, then materializes
// chat and issue actions.
func (h *Handler) ApplyOrchestratorDecision(ctx context.Context, orch db.RoomOrchestration, agentID pgtype.UUID, decisionJSON string) {
	var decision roomPlanDecision
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		slog.Warn("orchestrator: failed to parse decision JSON", "orch_id", uuidToString(orch.ID), "error", err)
		_, _ = h.Queries.UpdateRoomOrchestrationFailed(ctx, db.UpdateRoomOrchestrationFailedParams{
			ID:    orch.ID,
			Error: pgtype.Text{String: "invalid orchestrator decision JSON", Valid: true},
		})
		return
	}

	room, err := h.Queries.GetWorkspaceRoomByID(ctx, orch.RoomID)
	if err != nil {
		_, _ = h.Queries.UpdateRoomOrchestrationFailed(ctx, db.UpdateRoomOrchestrationFailedParams{
			ID:    orch.ID,
			Error: pgtype.Text{String: "failed to load room", Valid: true},
		})
		return
	}
	sourceMsg, err := h.Queries.GetRoomMessageInRoom(ctx, db.GetRoomMessageInRoomParams{
		ID:     orch.SourceMessageID,
		RoomID: orch.RoomID,
	})
	if err != nil {
		_, _ = h.Queries.UpdateRoomOrchestrationFailed(ctx, db.UpdateRoomOrchestrationFailedParams{
			ID:    orch.ID,
			Error: pgtype.Text{String: "failed to load source room message", Valid: true},
		})
		return
	}

	agentNameMap, err := h.buildRoomAgentNameMapCtx(ctx, room)
	if err != nil {
		_, _ = h.Queries.UpdateRoomOrchestrationFailed(ctx, db.UpdateRoomOrchestrationFailedParams{
			ID:    orch.ID,
			Error: pgtype.Text{String: "failed to load room agents", Valid: true},
		})
		return
	}
	decision = h.normalizeRoomPlanDecision(orch, decision, agentNameMap)
	if len(decision.Actions) == 0 {
		_, _ = h.Queries.UpdateRoomOrchestrationDecision(ctx, db.UpdateRoomOrchestrationDecisionParams{
			ID:           orch.ID,
			DecisionType: "plan",
			DecisionJson: []byte(decisionJSON),
		})
		return
	}

	creatorID := sourceMsg.SenderID
	prefix := h.getIssuePrefix(ctx, room.WorkspaceID)
	decisionBytes, _ := json.Marshal(decision)

	broadcastFn := func(issue db.Issue, _ []db.Attachment) map[string]any {
		return map[string]any{"issue": issueToResponse(issue, prefix)}
	}
	actorID := uuidToString(creatorID)

	_, err = h.Queries.UpdateRoomOrchestrationDecision(ctx, db.UpdateRoomOrchestrationDecisionParams{
		ID:           orch.ID,
		DecisionType: "plan",
		DecisionJson: decisionBytes,
	})
	if err != nil {
		_, _ = h.Queries.UpdateRoomOrchestrationFailed(ctx, db.UpdateRoomOrchestrationFailedParams{
			ID:    orch.ID,
			Error: pgtype.Text{String: err.Error(), Valid: true},
		})
		return
	}

	keyToIssueID := map[string]pgtype.UUID{}
	for _, action := range decision.Actions {
		action = normalizeRoomPlanAction(action)
		agentUUID := agentNameMap[strings.ToLower(action.Agent)]
		if !agentUUID.Valid {
			agentUUID = agentNameMap[strings.ToLower(normalizeRoomName(action.Agent))]
		}
		depsJSON, _ := json.Marshal(action.DependsOn)
		switch action.Mode {
		case "chat":
			session, err := h.createRoomActionChat(ctx, room, sourceMsg, action, agentUUID, creatorID)
			if err != nil {
				slog.Warn("orchestrator: failed to create chat action", "key", action.Key, "error", err)
				continue
			}
			_, _ = h.Queries.CreateRoomOrchestrationAction(ctx, db.CreateRoomOrchestrationActionParams{
				OrchestrationID: orch.ID,
				ActionKey:       action.Key,
				Mode:            "chat",
				AgentID:         agentUUID,
				Title:           action.Title,
				Stage:           action.Stage,
				Status:          "running",
				Deliverable:     action.Deliverable,
				DependsOn:       depsJSON,
				ChatSessionID:   session.ID,
			})
		case "issue":
			status := "todo"
			actionStatus := "running"
			if len(action.DependsOn) > 0 {
				status = "backlog"
				actionStatus = "blocked"
			}
			var assigneeType pgtype.Text
			if agentUUID.Valid {
				assigneeType = pgtype.Text{String: "agent", Valid: true}
			}
			res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
				WorkspaceID:    room.WorkspaceID,
				Title:          action.Title,
				Description:    pgtype.Text{String: roomActionDescription(sourceMsg.Content, action), Valid: true},
				Status:         status,
				Priority:       "none",
				AssigneeType:   assigneeType,
				AssigneeID:     agentUUID,
				CreatorType:    "member",
				CreatorID:      creatorID,
				OriginType:     pgtype.Text{String: "room_message", Valid: true},
				OriginID:       sourceMsg.ID,
				Stage:          pgtype.Int4{Int32: action.Stage, Valid: true},
				AllowDuplicate: true,
			}, service.IssueCreateOpts{
				ActorID:          actorID,
				BroadcastPayload: broadcastFn,
			})
			if err != nil {
				slog.Warn("orchestrator: failed to create issue action", "key", action.Key, "error", err)
				continue
			}
			keyToIssueID[action.Key] = res.Issue.ID
			_, _ = h.Queries.CreateRoomIssueLink(ctx, db.CreateRoomIssueLinkParams{
				RoomID:          orch.RoomID,
				RoomMessageID:   orch.SourceMessageID,
				OrchestrationID: orch.ID,
				IssueID:         res.Issue.ID,
				LinkRole:        "child",
			})
			_, _ = h.Queries.CreateRoomOrchestrationAction(ctx, db.CreateRoomOrchestrationActionParams{
				OrchestrationID: orch.ID,
				ActionKey:       action.Key,
				Mode:            "issue",
				AgentID:         agentUUID,
				Title:           action.Title,
				Stage:           action.Stage,
				Status:          actionStatus,
				Deliverable:     action.Deliverable,
				DependsOn:       depsJSON,
				IssueID:         res.Issue.ID,
			})
		}
	}
	for _, action := range decision.Actions {
		toID, ok := keyToIssueID[action.Key]
		if !ok {
			continue
		}
		for _, dep := range action.DependsOn {
			fromID, fromOK := keyToIssueID[dep]
			if !fromOK {
				continue
			}
			_, _ = h.Queries.InsertIssueDependency(ctx, db.InsertIssueDependencyParams{
				IssueID:          toID,
				DependsOnIssueID: fromID,
				Type:             "blocked_by",
			})
		}
	}
}

func normalizeRoomPlanAction(action roomPlanAction) roomPlanAction {
	action.Key = strings.TrimSpace(action.Key)
	action.Mode = strings.TrimSpace(strings.ToLower(action.Mode))
	action.Agent = strings.TrimSpace(action.Agent)
	action.Title = strings.TrimSpace(action.Title)
	action.Deliverable = strings.TrimSpace(action.Deliverable)
	if action.Mode == "" {
		action.Mode = "issue"
	}
	if action.Mode != "chat" && action.Mode != "issue" {
		action.Mode = "issue"
	}
	if action.Stage < 1 {
		action.Stage = 1
	}
	if action.Key == "" {
		action.Key = normalizeRoomName(action.Title)
	}
	if action.Title == "" {
		action.Title = action.Key
	}
	return action
}

func (h *Handler) normalizeRoomPlanDecision(orch db.RoomOrchestration, decision roomPlanDecision, agentNameMap map[string]pgtype.UUID) roomPlanDecision {
	decision.Type = strings.TrimSpace(strings.ToLower(decision.Type))
	if decision.Type == "" {
		decision.Type = "plan"
	}
	if len(decision.Actions) == 0 && len(decision.Nodes) > 0 {
		decision.Actions = decision.Nodes
		for i := range decision.Actions {
			decision.Actions[i].Mode = "issue"
		}
		for _, edge := range decision.Edges {
			for i := range decision.Actions {
				if decision.Actions[i].Key == edge.To {
					decision.Actions[i].DependsOn = append(decision.Actions[i].DependsOn, edge.From)
				}
			}
		}
	}
	if len(decision.Actions) == 0 && decision.Type == "single_issue" {
		decision.Actions = []roomPlanAction{{
			Key:   "single_issue",
			Mode:  "issue",
			Agent: decision.Agent,
			Title: decision.Title,
			Stage: 1,
		}}
	}
	if len(decision.Actions) == 0 && (decision.Type == "chat_only" || decision.Type == "plan") {
		for i, name := range mentionedAgentNamesFromInputSnapshot(orch.InputSnapshot, agentNameMap) {
			decision.Actions = append(decision.Actions, roomPlanAction{
				Key:   fmt.Sprintf("chat_%d_%s", i+1, normalizeRoomName(name)),
				Mode:  "chat",
				Agent: name,
				Title: "Reply in room chat",
				Stage: 1,
			})
		}
	}
	decision.Type = "plan"
	for i := range decision.Actions {
		decision.Actions[i] = normalizeRoomPlanAction(decision.Actions[i])
	}
	return decision
}

func mentionedAgentNamesFromInputSnapshot(input []byte, agentNameMap map[string]pgtype.UUID) []string {
	var snapshot struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(input, &snapshot)
	for _, line := range strings.Split(snapshot.Content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Agents mentioned:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "Agents mentioned:"))
		parts := strings.Split(raw, ",")
		out := []string{}
		for _, part := range parts {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if agentNameMap[strings.ToLower(name)].Valid || agentNameMap[strings.ToLower(normalizeRoomName(name))].Valid {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

func roomActionDescription(source string, action roomPlanAction) string {
	parts := []string{"Room request:", strings.TrimSpace(source)}
	if action.Deliverable != "" {
		parts = append(parts, "", "Expected deliverable:", action.Deliverable)
	}
	if len(action.DependsOn) > 0 {
		parts = append(parts, "", "Wait for room action outputs:", strings.Join(action.DependsOn, ", "))
	}
	return strings.Join(parts, "\n")
}

func (h *Handler) createRoomActionChat(ctx context.Context, room db.WorkspaceRoom, sourceMsg db.RoomMessage, action roomPlanAction, agentID pgtype.UUID, creatorID pgtype.UUID) (db.ChatSession, error) {
	memberNameMap := h.buildRoomSenderNamesCtx(ctx, room)
	recentDesc, _ := h.Queries.ListRecentRoomMessages(ctx, db.ListRecentRoomMessagesParams{
		RoomID: room.ID,
		Limit:  21,
	})
	var contextLines []string
	for i := len(recentDesc) - 1; i >= 0; i-- {
		m := recentDesc[i]
		if uuidToString(m.ID) == uuidToString(sourceMsg.ID) || m.SenderType == "system" {
			continue
		}
		name := memberNameMap[uuidToString(m.SenderID)]
		if name == "" {
			name = m.SenderType
		}
		contextLines = append(contextLines, name+": "+m.Content)
	}
	userContent := sourceMsg.Content
	if action.Deliverable != "" {
		userContent += "\n\nExpected deliverable: " + action.Deliverable
	}
	if len(contextLines) > 0 {
		userContent = "[Recent room chat]\n" + strings.Join(contextLines, "\n") + "\n\n" + userContent
	}
	session, err := h.Queries.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: room.WorkspaceID,
		AgentID:     agentID,
		CreatorID:   creatorID,
		Title:       action.Title,
	})
	if err != nil {
		return db.ChatSession{}, err
	}
	chatMsg, err := h.Queries.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       userContent,
	})
	if err != nil {
		return db.ChatSession{}, err
	}
	task, err := h.TaskService.EnqueueChatTask(ctx, session, creatorID, false)
	if err != nil {
		return db.ChatSession{}, err
	}
	_ = h.Queries.LinkChatMessageToTask(ctx, db.LinkChatMessageToTaskParams{
		ID:     chatMsg.ID,
		TaskID: task.ID,
	})
	return session, nil
}

func (h *Handler) buildRoomSenderNamesCtx(ctx context.Context, room db.WorkspaceRoom) map[string]string {
	out := make(map[string]string)
	members, err := h.Queries.ListRoomMembers(ctx, room.ID)
	if err != nil {
		return out
	}
	for _, m := range members {
		id := uuidToString(m.MemberID)
		switch m.MemberType {
		case "agent":
			if agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: m.MemberID, WorkspaceID: room.WorkspaceID}); err == nil {
				out[id] = agent.Name
			}
		case "member":
			if member, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: m.MemberID, WorkspaceID: room.WorkspaceID}); err == nil {
				if user, err := h.Queries.GetUser(ctx, member.UserID); err == nil {
					out[id] = user.Name
				}
			}
		}
	}
	return out
}

func (h *Handler) CompleteRoomChatAction(ctx context.Context, action db.RoomOrchestrationAction, agentID pgtype.UUID, output string) {
	if action.Status == "done" {
		return
	}
	completed, err := h.Queries.CompleteRoomOrchestrationAction(ctx, db.CompleteRoomOrchestrationActionParams{
		ID:     action.ID,
		Output: strings.TrimSpace(output),
	})
	if err != nil {
		slog.Warn("room action: failed to complete chat action", "action_id", uuidToString(action.ID), "error", err)
		return
	}
	h.advanceRoomOrchestrationActions(ctx, completed.OrchestrationID)
}

func (h *Handler) CompleteRoomIssueActionAndAdvance(ctx context.Context, issue db.Issue) {
	action, err := h.Queries.GetRoomOrchestrationActionByIssue(ctx, issue.ID)
	if err != nil || action.Status == "done" {
		return
	}
	output := fmt.Sprintf("Issue %s reached %s.", issue.Title, issue.Status)
	completed, err := h.Queries.CompleteRoomOrchestrationAction(ctx, db.CompleteRoomOrchestrationActionParams{
		ID:     action.ID,
		Output: output,
	})
	if err != nil {
		slog.Warn("room action: failed to complete issue action", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	h.advanceRoomOrchestrationActions(ctx, completed.OrchestrationID)
}

func roomIssueActionSatisfied(status string) bool {
	switch status {
	case "waiting_review", "in_review", "done":
		return true
	default:
		return false
	}
}

func (h *Handler) advanceRoomOrchestrationActions(ctx context.Context, orchestrationID pgtype.UUID) {
	actions, err := h.Queries.ListRoomOrchestrationActions(ctx, orchestrationID)
	if err != nil {
		return
	}
	done := map[string]bool{}
	outputs := map[string]string{}
	for _, action := range actions {
		if action.Status == "done" {
			done[action.ActionKey] = true
			outputs[action.ActionKey] = action.Output
		}
	}
	for _, action := range actions {
		if action.Mode != "issue" || action.Status != "blocked" || !action.IssueID.Valid {
			continue
		}
		deps := roomActionDependsOn(action)
		ready := true
		for _, dep := range deps {
			if !done[dep] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		issue, err := h.Queries.GetIssue(ctx, action.IssueID)
		if err != nil || issue.Status != "backlog" {
			continue
		}
		updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID:          issue.ID,
			Status:      "todo",
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			slog.Warn("room action: failed to promote dependent issue", "issue_id", uuidToString(issue.ID), "error", err)
			continue
		}
		_, _ = h.Queries.UpdateRoomOrchestrationActionStatus(ctx, db.UpdateRoomOrchestrationActionStatusParams{
			ID:     action.ID,
			Status: "running",
		})
		prefix := h.getIssuePrefix(ctx, updated.WorkspaceID)
		h.publish(protocol.EventIssueUpdated, uuidToString(updated.WorkspaceID), "system", "", map[string]any{
			"issue":          issueToResponse(updated, prefix),
			"status_changed": true,
			"prev_status":    issue.Status,
		})
		if trigger, ok := h.IssueService.WillEnqueueRun(ctx, service.IssueTriggerInput{
			Issue:         updated,
			PrevStatus:    issue.Status,
			StatusChanged: true,
		}, service.IssueTriggerProbe{}); ok {
			h.dispatchIssueRun(ctx, updated, trigger, "system", "", roomDependencyHandoff(deps, outputs))
		}
	}
}

func roomActionDependsOn(action db.RoomOrchestrationAction) []string {
	var deps []string
	_ = json.Unmarshal(action.DependsOn, &deps)
	return deps
}

func roomDependencyHandoff(deps []string, outputs map[string]string) string {
	if len(deps) == 0 {
		return ""
	}
	lines := []string{"Room plan dependencies completed:"}
	for _, dep := range deps {
		output := strings.TrimSpace(outputs[dep])
		if output == "" {
			output = "done"
		}
		lines = append(lines, "- "+dep+": "+output)
	}
	return strings.Join(lines, "\n")
}

func (h *Handler) issuesForLinks(r *http.Request, links []db.RoomIssueLink) []IssueResponse {
	prefix := ""
	out := []IssueResponse{}
	for _, link := range links {
		issue, err := h.Queries.GetIssue(r.Context(), link.IssueID)
		if err != nil {
			continue
		}
		if prefix == "" {
			prefix = h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		}
		out = append(out, issueToResponse(issue, prefix))
	}
	return out
}

func (h *Handler) workspaceUUIDFromContext(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	return wsUUID, ok
}

func (h *Handler) loadRoomForUser(w http.ResponseWriter, r *http.Request) (db.WorkspaceRoom, bool) {
	wsUUID, ok := h.workspaceUUIDFromContext(w, r)
	if !ok {
		return db.WorkspaceRoom{}, false
	}
	roomID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "roomId"), "room_id")
	if !ok {
		return db.WorkspaceRoom{}, false
	}
	room, err := h.Queries.GetRoomInWorkspace(r.Context(), db.GetRoomInWorkspaceParams{ID: roomID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return db.WorkspaceRoom{}, false
	}
	return room, true
}

func (h *Handler) requireWorkspaceAdmin(w http.ResponseWriter, r *http.Request) bool {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "workspace member required")
		return false
	}
	if member.Role != "owner" && member.Role != "admin" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

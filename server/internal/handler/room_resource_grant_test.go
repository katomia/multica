package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// withRoomTestMemberCtx injects the workspace+member context that the real chi
// middleware chain would set, so handlers called directly from tests (CreateRoom,
// AddRoomMember, …) can resolve the acting member. Mirrors withChatTestWorkspaceCtx.
func withRoomTestMemberCtx(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(testUserID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load test member row: %v", err)
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

// insertProjectFixture creates a bare project row in the test workspace and
// returns its id. Tests attach resources and bind rooms to it.
func insertProjectFixture(t *testing.T, title string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, $2, 'planned', 'none')
		RETURNING id
	`, testWorkspaceID, title).Scan(&projectID); err != nil {
		t.Fatalf("insert project fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

// insertProjectResourceFixture attaches a github_repo resource to a project and
// returns its id. Tests use this to seed a resource before room creation.
func insertProjectResourceFixture(t *testing.T, projectID, url string) string {
	t.Helper()
	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, position)
		VALUES ($1, $2, 'github_repo', jsonb_build_object('url', $3::text), 0)
		RETURNING id
	`, projectID, testWorkspaceID, url).Scan(&resourceID); err != nil {
		t.Fatalf("insert project_resource fixture: %v", err)
	}
	return resourceID
}

// findGrant locates the grant response for a given agent + resource, returning
// (grant, found). The grant matrix is small in tests, so a linear scan is fine.
func findGrant(grants []RoomResourceGrantResponse, agentID, resourceID string) (RoomResourceGrantResponse, bool) {
	for _, g := range grants {
		if g.AgentID == agentID && g.ResourceID == resourceID {
			return g, true
		}
	}
	return RoomResourceGrantResponse{}, false
}

// TestProjectBasedRoom_GrantBackfill covers the core MVP grant lifecycle:
// V1 create-from-project, V3 uniqueness, V4 member-add backfill, V5
// resource-add backfill, V6 access downgrade. It exercises the handler paths
// end-to-end against the migrated test DB.
func TestProjectBasedRoom_GrantBackfill(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "GrantBackfill Agent", []byte("[]"))
	projectID := insertProjectFixture(t, "GrantBackfill Project")
	resourceID1 := insertProjectResourceFixture(t, projectID, "https://github.com/multica-ai/grant-backfill-1")

	// V1: create a project-based room. The room binds to the project and seeds
	// default 'write' grants for the room's agents (the auto-created orchestrator
	// at minimum) against every project resource.
	w := httptest.NewRecorder()
	req := withRoomTestMemberCtx(t, newRequest(http.MethodPost, "/api/rooms", map[string]any{
		"name":       "grant-backfill-room",
		"project_id": projectID,
	}))
	testHandler.CreateRoom(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateRoom: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var room RoomResponse
	if err := json.NewDecoder(w.Body).Decode(&room); err != nil {
		t.Fatalf("decode room: %v", err)
	}
	if room.ProjectID == nil || *room.ProjectID != projectID {
		t.Fatalf("CreateRoom: expected project_id=%s, got %v", projectID, room.ProjectID)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_room WHERE id = $1`, room.ID)
	})

	// V4: adding an agent member backfills write grants for that agent against
	// every project resource.
	w = httptest.NewRecorder()
	req = withRoomTestMemberCtx(t, newRequest(http.MethodPost, "/api/rooms/"+room.ID+"/members", map[string]any{
		"member_type": "agent",
		"member_id":   agentID,
	}))
	req = withURLParam(req, "roomId", room.ID)
	testHandler.AddRoomMember(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("AddRoomMember: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	grants := listGrants(t, room.ID)
	grant1, ok := findGrant(grants, agentID, resourceID1)
	if !ok {
		t.Fatalf("expected grant for agent=%s resource=%s after member add, got %v", agentID, resourceID1, grants)
	}
	if grant1.AccessLevel != "write" {
		t.Fatalf("expected default access_level=write, got %q", grant1.AccessLevel)
	}

	// V5: adding a new project resource backfills a write grant for every room
	// agent (including the one added above) against the new resource.
	resourceID2 := insertProjectResourceFixture(t, projectID, "https://github.com/multica-ai/grant-backfill-2")
	// The resource was inserted via SQL directly (bypassing the handler hook),
	// so simulate the handler path by calling CreateProjectResource on a fresh
	// resource to exercise the hook. Use a third URL to avoid the unique conflict.
	w = httptest.NewRecorder()
	createReq := withRoomTestMemberCtx(t, newRequest(http.MethodPost, "/api/projects/"+projectID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/grant-backfill-3"},
	}))
	createReq = withURLParam(createReq, "id", projectID)
	testHandler.CreateProjectResource(w, createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createdResource ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&createdResource); err != nil {
		t.Fatalf("decode resource: %v", err)
	}
	resourceID3 := createdResource.ID

	grants = listGrants(t, room.ID)
	grant3, ok := findGrant(grants, agentID, resourceID3)
	if !ok {
		t.Fatalf("expected grant for new resource after CreateProjectResource, got %v", grants)
	}
	if grant3.AccessLevel != "write" {
		t.Fatalf("expected new-resource default access_level=write, got %q", grant3.AccessLevel)
	}
	// The pre-existing grant on resource 1 must be untouched (still write).
	if g1, ok := findGrant(grants, agentID, resourceID1); !ok || g1.AccessLevel != "write" {
		t.Fatalf("existing grant should remain write, got %v", g1)
	}
	_ = resourceID2 // fixture inserted via SQL; not asserted

	// V6: downgrade the grant on resource 3 from write to read. Other grants
	// for the same agent must be unaffected.
	w = httptest.NewRecorder()
	patchReq := withRoomTestMemberCtx(t, newRequest(http.MethodPatch, "/api/rooms/"+room.ID+"/resource-grants/"+grant3.ID, map[string]any{
		"access_level": "read",
	}))
	patchReq = withURLParams(patchReq, "roomId", room.ID, "grantId", grant3.ID)
	testHandler.UpdateRoomResourceGrant(w, patchReq)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateRoomResourceGrant: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated RoomResourceGrantResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated grant: %v", err)
	}
	if updated.AccessLevel != "read" {
		t.Fatalf("expected access_level=read after patch, got %q", updated.AccessLevel)
	}
	// Reload and confirm only the patched grant changed.
	grants = listGrants(t, room.ID)
	if g3, _ := findGrant(grants, agentID, resourceID3); g3.AccessLevel != "read" {
		t.Fatalf("reloaded grant 3 should be read, got %q", g3.AccessLevel)
	}
	if g1, _ := findGrant(grants, agentID, resourceID1); g1.AccessLevel != "write" {
		t.Fatalf("grant 1 should still be write, got %q", g1.AccessLevel)
	}

	// V3: a second project-based room for the same project must 409. The partial
	// unique index (plus the pre-check) enforces one room per project.
	w = httptest.NewRecorder()
	req = withRoomTestMemberCtx(t, newRequest(http.MethodPost, "/api/rooms", map[string]any{
		"name":       "grant-backfill-room-dup",
		"project_id": projectID,
	}))
	testHandler.CreateRoom(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate project room: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_room WHERE name = $1 AND workspace_id = $2`, "grant-backfill-room-dup", testWorkspaceID)
	})

	// V3 (cont.): the project must no longer appear in ?without_room=true.
	w = httptest.NewRecorder()
	listReq := newRequest(http.MethodGet, "/api/projects?without_room=true", nil)
	testHandler.ListProjects(w, listReq)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects without_room: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	for _, p := range listResp.Projects {
		if p.ID == projectID {
			t.Fatalf("project with a room should not appear in without_room=true list")
		}
	}
}

// listGrants calls ListRoomResourceGrants and decodes the response, failing the
// test on any non-200 so callers can assume a valid grant slice.
func listGrants(t *testing.T, roomID string) []RoomResourceGrantResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/rooms/"+roomID+"/resource-grants", nil), "roomId", roomID)
	testHandler.ListRoomResourceGrants(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListRoomResourceGrants: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var grants []RoomResourceGrantResponse
	if err := json.NewDecoder(w.Body).Decode(&grants); err != nil {
		t.Fatalf("decode grants: %v", err)
	}
	return grants
}

package server_test

import (
	"strings"
	"testing"
	"time"

	"github.com/oduvan/webquiz-go/internal/testutil"
)

func paApprovalServer(t *testing.T, approve bool, fields ...any) *testutil.TestServer {
	t.Helper()
	registration := map[string]any{"approve": approve}
	if len(fields) > 0 {
		registration["fields"] = fields
	}
	return testutil.StartServer(t, testutil.Options{Config: map[string]any{"registration": registration}})
}

func paApproveUser(t *testing.T, ts *testutil.TestServer, userID string, opts ...testutil.RequestOption) *testutil.Response {
	t.Helper()
	return ts.Put(t, "/api/admin/approve-user", map[string]any{"user_id": userID}, opts...)
}

// py: tests/test_registration_approval.py::test_register_with_approve_disabled
func TestRegisterWithApproveDisabled(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, false)
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "TestUser"})
	data := res.JSON(t)
	if res.Status != 200 || data["username"] != "TestUser" || data["user_id"] == nil ||
		data["requires_approval"] != false || data["approved"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_register_with_approve_enabled
func TestRegisterWithApproveEnabled(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "TestUser"})
	data := res.JSON(t)
	if res.Status != 200 || data["username"] != "TestUser" || data["user_id"] == nil ||
		data["requires_approval"] != true || data["approved"] != false {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_register_with_approve_enabled_and_custom_fields
func TestRegisterWithApproveEnabledAndCustomFields(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true, "Grade", "School")
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "TestUser", "grade": "10", "school": "Central HS"})
	data := res.JSON(t)
	if res.Status != 200 || data["username"] != "TestUser" || data["requires_approval"] != true || data["approved"] != false {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// The Python test only checked the registration reply (it did not connect
// to the socket). This port also reads the admin socket.
// py: tests/test_registration_approval.py::test_register_broadcasts_to_admin_websocket
func TestRegisterBroadcastsToAdminWebsocket(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	admin := openSocket(t, ts, "/ws/admin")
	admin.next() // initial_state
	res := ts.PostJSON(t, "/api/register", map[string]any{"username": "TestUser"})
	if res.Status != 200 || res.JSON(t)["approved"] != false {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if message := admin.next(); message["type"] != "new_registration" {
		t.Errorf("admin socket got %v", message)
	}
}

// py: tests/test_registration_approval.py::test_verify_user_not_approved
func TestVerifyUserNotApproved(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	userID := ts.RegisterStudent(t, "TestUser")
	res := ts.GetJSON(t, "/api/verify-user/"+userID)
	data := res.JSON(t)
	if res.Status != 200 || data["valid"] != true || data["approved"] != false || data["requires_approval"] != true || data["user_data"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_verify_user_approved
func TestVerifyUserApproved(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	if res := paApproveUser(t, ts, userID, testutil.WithCookies(cookies...)); res.Status != 200 {
		t.Fatalf("approve: %d %s", res.Status, res.Body)
	}
	res := ts.GetJSON(t, "/api/verify-user/"+userID)
	data := res.JSON(t)
	if res.Status != 200 || data["valid"] != true || data["approved"] != true {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_verify_user_approved_after_approval
func TestVerifyUserApprovedAfterApproval(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	if got := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)["approved"]; got != false {
		t.Errorf("before approval: approved = %v", got)
	}
	paApproveUser(t, ts, userID, testutil.WithCookies(cookies...))
	if got := ts.GetJSON(t, "/api/verify-user/"+userID).JSON(t)["approved"]; got != true {
		t.Errorf("after approval: approved = %v", got)
	}
}

// py: tests/test_registration_approval.py::test_update_registration_while_waiting
func TestUpdateRegistrationWhileWaiting(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true, "Grade")
	userID := partARegister(t, ts, map[string]any{"username": "OldName", "grade": "9"})["user_id"].(string)
	res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": userID, "username": "NewName", "grade": "10"})
	data := res.JSON(t)
	userData, _ := data["user_data"].(map[string]any)
	if res.Status != 200 || data["success"] != true || userData["username"] != "NewName" || userData["grade"] != "10" {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_update_registration_after_approval
func TestUpdateRegistrationAfterApproval(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	paApproveUser(t, ts, userID, testutil.WithCookies(cookies...))
	res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": userID, "username": "NewName"})
	text, _ := res.JSON(t)["error"].(string)
	if res.Status != 400 || !strings.Contains(text, "Cannot update registration data after approval") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_update_registration_invalid_user
func TestUpdateRegistrationInvalidUser(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": "999999", "username": "TestUser"})
	text, _ := res.JSON(t)["error"].(string)
	if res.Status != 404 || !strings.Contains(text, "User not found") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_update_registration_validates_required_fields
func TestUpdateRegistrationValidatesRequiredFields(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	ts.RegisterStudent(t, "User1")
	user2 := ts.RegisterStudent(t, "User2")
	res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": user2, "username": "User1"})
	if res.Status != 400 || res.JSON(t)["error"] == nil {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_update_registration_broadcasts_websocket
func TestUpdateRegistrationBroadcastsWebsocket(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	userID := ts.RegisterStudent(t, "TestUser")
	admin := openSocket(t, ts, "/ws/admin")
	admin.next() // initial_state
	res := ts.Put(t, "/api/update-registration", map[string]any{"user_id": userID, "username": "UpdatedUser"})
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// The Python test did not read the socket; this port does.
	if message := admin.next(); message["type"] != "registration_updated" {
		t.Errorf("admin socket got %v", message)
	}
}

// py: tests/test_registration_approval.py::test_approve_user_requires_auth
func TestApproveUserRequiresAuth(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	userID := ts.RegisterStudent(t, "TestUser")
	if res := paApproveUser(t, ts, userID); res.Status != 401 {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_approve_user_successfully
func TestApproveUserSuccessfully(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	res := paApproveUser(t, ts, userID, testutil.WithCookies(cookies...))
	data := res.JSON(t)
	message, _ := data["message"].(string)
	if res.Status != 200 || data["success"] != true || !strings.Contains(message, "User approved successfully") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_approve_user_starts_timing
func TestApproveUserStartsTiming(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	time.Sleep(100 * time.Millisecond)
	if res := paApproveUser(t, ts, userID, testutil.WithCookies(cookies...)); res.Status != 200 {
		t.Fatalf("approve: %d %s", res.Status, res.Body)
	}
	res := partAAnswer(t, ts, userID, 1, 1)
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if taken := paFloatField(t, res.JSON(t), "time_taken"); taken < 0 {
		t.Errorf("time_taken = %v", taken)
	}
}

// py: tests/test_registration_approval.py::test_approve_user_initializes_live_stats
func TestApproveUserInitializesLiveStats(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	res := paApproveUser(t, ts, userID, testutil.WithCookies(cookies...))
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// The Python test stopped here. This port also checks that the live
	// stats show the student after the approval.
	stats := liveStatsState(t, ts)
	if !strings.Contains(jsonText(stats), userID) {
		t.Errorf("live stats do not list %s: %v", userID, stats)
	}
}

// py: tests/test_registration_approval.py::test_approve_user_invalid_user_id
func TestApproveUserInvalidUserID(t *testing.T) {
	t.Parallel()
	ts := testutil.StartServer(t, testutil.Options{})
	cookies := ts.AdminCookies(t, "test123")
	res := paApproveUser(t, ts, "999999", testutil.WithCookies(cookies...))
	text, _ := res.JSON(t)["error"].(string)
	if res.Status != 404 || !strings.Contains(text, "User not found") {
		t.Errorf("%d %s", res.Status, res.Body)
	}
}

// py: tests/test_registration_approval.py::test_approve_user_broadcasts_websocket
func TestApproveUserBroadcastsWebsocket(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	admin := openSocket(t, ts, "/ws/admin")
	admin.next()
	res := paApproveUser(t, ts, userID, testutil.WithCookies(cookies...))
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	// The Python test did not read the socket; this port does.
	if message := admin.next(); message["type"] != "user_approved" {
		t.Errorf("admin socket got %v", message)
	}
}

// py: tests/test_registration_approval.py::test_timing_starts_on_question_start_when_approve_false
func TestTimingStartsOnQuestionStartWhenApproveFalse(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, false)
	userID := ts.RegisterStudent(t, "TestUser")
	time.Sleep(1500 * time.Millisecond) // time before the page shows the question does not count
	ts.PostJSON(t, "/api/question-start", map[string]any{"user_id": userID, "question_id": 1})
	time.Sleep(200 * time.Millisecond)
	res := partAAnswer(t, ts, userID, 1, 1)
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	taken := paFloatField(t, res.JSON(t), "time_taken")
	if taken < 0.2 || taken >= 1.2 {
		t.Errorf("time_taken = %v, want 0.2 <= t < 1.2", taken)
	}
}

// py: tests/test_registration_approval.py::test_timing_starts_on_approval_when_approve_true
func TestTimingStartsOnApprovalWhenApproveTrue(t *testing.T) {
	t.Parallel()
	ts := paApprovalServer(t, true)
	cookies := ts.AdminCookies(t, "test123")
	userID := ts.RegisterStudent(t, "TestUser")
	time.Sleep(500 * time.Millisecond)
	if res := paApproveUser(t, ts, userID, testutil.WithCookies(cookies...)); res.Status != 200 {
		t.Fatalf("approve: %d %s", res.Status, res.Body)
	}
	time.Sleep(100 * time.Millisecond)
	res := partAAnswer(t, ts, userID, 1, 1)
	if res.Status != 200 {
		t.Fatalf("%d %s", res.Status, res.Body)
	}
	if taken := paFloatField(t, res.JSON(t), "time_taken"); taken >= 0.3 {
		t.Errorf("time_taken = %v, want < 0.3", taken)
	}
}

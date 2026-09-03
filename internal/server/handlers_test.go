package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alveel/quorum/internal/absence"
	"github.com/alveel/quorum/internal/config"
	"github.com/alveel/quorum/internal/store"
)

// newTestServer wires a chi router with dev auth bypass and the given store fake.
func newTestServer(st Storer) *httptest.Server {
	cfg := config.Config{
		DevAuthBypass: true,
		DevUser:       "testuser",
		DevAdmin:      true,
		Port:          "8080",
	}
	// Use current directory as static FS; handler tests don't exercise static assets.
	h := New(cfg, st, os.DirFS("."))
	return httptest.NewServer(h)
}

// --- createAbsence ---

func TestCreateAbsence_NotConfigured_Returns422(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/absences",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-06&end_date=2026-07-06"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Settings") {
		t.Errorf("body missing configure-first message: %s", body)
	}
}

func TestCreateAbsence_InvalidDates_Returns422(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{MinPresent: 8}, member: testUserMember()})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/absences",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=notadate&end_date=2026-07-14"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
}

func TestCreateAbsence_ThresholdDenied_Returns422(t *testing.T) {
	// 14 other roster members absent Jul 7 (Tue); adding requester's candidate absence
	// drops present to 0 < min=8.
	jul7 := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	roster := oneRoleRoster()
	var others []absence.Absence
	for i := 1; i < len(roster); i++ {
		others = append(others, absence.Absence{
			UserID: roster[i].ID, StartDate: jul7, EndDate: jul7, Status: absence.StatusApproved,
		})
	}
	st := &fakeStore{
		settings:        absence.Settings{MinPresent: 8},
		member:          testUserMember(),
		roster:          roster,
		roles:           oneRole(),
		absencesInRange: others,
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/absences",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-07&end_date=2026-07-07"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Coverage would drop below minimum") {
		t.Errorf("body missing denial message: %s", body)
	}
}

func TestCreateAbsence_Success_Returns200WithOOBSwaps(t *testing.T) {
	st := &fakeStore{
		settings: absence.Settings{MinPresent: 8},
		member:   testUserMember(),
		roster:   oneRoleRoster(),
		roles:    oneRole(),
		createVac: absence.Absence{
			UserID:    "testuser",
			StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
			Status:    absence.StatusApproved,
		},
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/absences",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-01&end_date=2026-07-05"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `id="heatmap"`) {
		t.Error("response missing heatmap section")
	}
	if !strings.Contains(bodyStr, `hx-swap-oob`) {
		t.Error("response missing hx-swap-oob attribute")
	}
	if !strings.Contains(bodyStr, `id="my-absences"`) {
		t.Error("response missing my-absences section")
	}
}

func TestCreateAbsence_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings:     absence.Settings{MinPresent: 8},
		member:       testUserMember(),
		roster:       oneRoleRoster(),
		roles:        oneRole(),
		createVacErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/absences",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-01&end_date=2026-07-05"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestCreateAbsence_Overlap_Returns422(t *testing.T) {
	st := &fakeStore{
		settings:   absence.Settings{MinPresent: 8},
		member:     testUserMember(),
		hasOverlap: true,
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/absences",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-01&end_date=2026-07-05"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "already have leave") {
		t.Errorf("body missing overlap message: %s", body)
	}
}

// --- adminOverride ---

func TestAdminOverride_Success_Returns303(t *testing.T) {
	st := &fakeStore{createOvr: absence.Absence{UserID: "alice", Status: absence.StatusOverridden}}
	ts := newTestServer(st)
	defer ts.Close()

	// Don't follow the redirect so we can assert the 303 + Location.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/admin/override", map[string][]string{
		"user_id":    {"alice"},
		"reason":     {"critical fix"},
		"start_date": {"2026-07-01"},
		"end_date":   {"2026-07-05"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/admin" {
		t.Errorf("Location = %q, want /admin", loc)
	}
}

func TestAdminOverride_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{createOvrErr: errors.New("db error")}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/admin/override", map[string][]string{
		"user_id":    {"alice"},
		"start_date": {"2026-07-01"},
		"end_date":   {"2026-07-05"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// --- cancelAbsence ---

func TestCancelAbsence_InvalidUUID_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/absences/not-a-uuid", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestCancelAbsence_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{cancelErr: errors.New("not found or not cancellable")}
	ts := newTestServer(st)
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/absences/00000000-0000-0000-0000-000000000001", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestCancelAbsence_Success_RendersFragments(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/absences/00000000-0000-0000-0000-000000000001", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `id="my-absences"`) {
		t.Error("response missing my-absences section")
	}
	if !strings.Contains(bodyStr, `id="heatmap"`) {
		t.Error("response missing heatmap OOB swap")
	}
}

// --- dayDetail ---

func TestDayDetail_BadDate_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/not-a-date")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestDayDetail_Success_RendersPanel(t *testing.T) {
	// Jul 6 2026 is a Monday — a normal working day for a Mon-Fri roster.
	st := &fakeStore{
		settings: absence.Settings{MinPresent: 8},
		roster:   fifteenMemberRoster(),
		onDay: []absence.Absence{
			{
				UserName:  "Alice",
				StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				EndDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
				Status:    absence.StatusApproved,
			},
		},
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/2026-07-06")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "day-detail-panel") {
		t.Errorf("response missing day-detail-panel: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Alice") {
		t.Error("response missing absence owner name")
	}
}

func TestDayDetail_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings: absence.Settings{MinPresent: 8},
		onDayErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/2026-07-06")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// --- adminPage ---

func TestAdminPage_GetSettingsError_Returns500(t *testing.T) {
	st := &fakeStore{settingsErr: errors.New("db error")}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestAdminPage_ListAllActiveError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings:     absence.Settings{MinPresent: 8},
		allActiveErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestAdminPage_Success_Returns200(t *testing.T) {
	st := &fakeStore{
		settings: absence.Settings{MinPresent: 8, HolidayCountry: "nl"},
		roster:   fifteenMemberRoster(),
		roles:    oneRole(),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

// --- adminSettings ---

func TestAdminSettings_InvalidMinPresent_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/admin/settings",
		"application/x-www-form-urlencoded",
		strings.NewReader("min_present=notanumber"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestAdminSettings_UnsupportedCountry_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/admin/settings",
		"application/x-www-form-urlencoded",
		strings.NewReader("min_present=8&holiday_country=zz"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestAdminSettings_UpdateError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings:         absence.Settings{MinPresent: 8},
		updateSettingErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/admin/settings",
		"application/x-www-form-urlencoded",
		strings.NewReader("min_present=8"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// --- member settings (self-service) ---

func TestMemberSettings_Get_Returns200(t *testing.T) {
	ts := newTestServer(&fakeStore{member: testUserMember(), roles: oneRole()})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

// TestMemberSettings_Post_UpdatesCallerNotArbitraryUser is the IDOR guard: even if the
// posted form includes a user_id field, the target must always be the authenticated
// dev-bypass user ("testuser"), never whatever the form claims.
func TestMemberSettings_Post_UpdatesCallerNotArbitraryUser(t *testing.T) {
	st := &fakeStore{roles: oneRole()}
	ts := newTestServer(st)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/settings", map[string][]string{
		"user_id": {"someone-else"},
		"wd":      {"1", "2", "3", "4", "5"},
		"role_id": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
	if st.updateMemberSettingsUserID != "testuser" {
		t.Errorf("UpdateMemberSettings target = %q, want %q (IDOR guard)", st.updateMemberSettingsUserID, "testuser")
	}
}

func TestMemberSettings_Post_UnknownRoleID_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{roles: oneRole()})
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/settings", map[string][]string{
		"role_id": {"999"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// --- admin roster ---

func TestAdminCreateRosterMember_Success_Returns303(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/admin/roster", map[string][]string{"id": {"bob"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
}

func TestAdminCreateRosterMember_MissingID_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/admin/roster", map[string][]string{"id": {""}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestAdminUpdateMember_TargetsURLParamUser(t *testing.T) {
	st := &fakeStore{roles: oneRole()}
	ts := newTestServer(st)
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/admin/roster/bob", map[string][]string{
		"display_name": {"Bob"},
		"active":       {"true"},
		"wd":           {"1", "2", "3", "4", "5"},
		"role_id":      {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
	if st.updateMemberSettingsUserID != "bob" {
		t.Errorf("UpdateMemberSettings target = %q, want %q", st.updateMemberSettingsUserID, "bob")
	}
}

// --- admin roles ---

func TestAdminCreateRole_Success_Returns303(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/admin/roles", map[string][]string{
		"name": {"DBA"}, "min_present": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
}

func TestAdminDeleteRole_Success_Returns303(t *testing.T) {
	ts := newTestServer(&fakeStore{})
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/admin/roles/1/delete", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
}

// --- admin holidays ---

func TestAdminAddHoliday_DuplicateDate_Returns422(t *testing.T) {
	st := &fakeStore{createHolidayErr: store.ErrDuplicateHoliday}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/admin/holidays", map[string][]string{
		"date": {"2026-01-01"}, "name": {"New Year"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
}

func TestAdminSyncHolidays_NoCountryConfigured_Returns422(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{MinPresent: 8}})
	defer ts.Close()

	resp, err := http.PostForm(ts.URL+"/admin/holidays/sync", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
}

func TestAdminSyncHolidays_Success_Returns303(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{MinPresent: 8, HolidayCountry: "nl"}})
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.PostForm(ts.URL+"/admin/holidays/sync", map[string][]string{"year": {"2026"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("want 303, got %d", resp.StatusCode)
	}
}

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

func TestCreateAbsence_InvalidDates_Returns422(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{TeamSize: 15, MinPresent: 8}})
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
	// 14 have absence Jul 7 → 1 present; adding requester → 0 < min=8.
	jul7 := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	st := &fakeStore{
		settings: absence.Settings{TeamSize: 15, MinPresent: 8, WeekendCounts: true},
		perDay:   map[time.Time]int{jul7: 14},
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
		settings: absence.Settings{TeamSize: 15, MinPresent: 8},
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
		settings:     absence.Settings{TeamSize: 15, MinPresent: 8},
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
		settings:   absence.Settings{TeamSize: 15, MinPresent: 8},
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
	st := &fakeStore{
		settings:  absence.Settings{TeamSize: 15, MinPresent: 8},
		createOvr: absence.Absence{UserID: "alice", Status: absence.StatusOverridden},
	}
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
	st := &fakeStore{
		settings:     absence.Settings{TeamSize: 15, MinPresent: 8},
		createOvrErr: errors.New("db error"),
	}
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
	ts := newTestServer(&fakeStore{settings: absence.Settings{TeamSize: 15, MinPresent: 8}})
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
	st := &fakeStore{
		settings:  absence.Settings{TeamSize: 15, MinPresent: 8},
		cancelErr: errors.New("not found or not cancellable"),
	}
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
	ts := newTestServer(&fakeStore{settings: absence.Settings{TeamSize: 15, MinPresent: 8}})
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
	ts := newTestServer(&fakeStore{settings: absence.Settings{TeamSize: 15, MinPresent: 8}})
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
	st := &fakeStore{
		settings: absence.Settings{TeamSize: 15, MinPresent: 8},
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

	resp, err := http.Get(ts.URL + "/day/2026-07-05")
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
		settings: absence.Settings{TeamSize: 15, MinPresent: 8},
		onDayErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/2026-07-05")
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
		settings:     absence.Settings{TeamSize: 15, MinPresent: 8},
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

// --- adminSettings ---

func TestAdminSettings_InvalidMinPresent_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{TeamSize: 15, MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/admin/settings",
		"application/x-www-form-urlencoded",
		strings.NewReader("min_present=notanumber&team_size=15&weekend_counts=false"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestAdminSettings_InvalidTeamSize_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: absence.Settings{TeamSize: 15, MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/admin/settings",
		"application/x-www-form-urlencoded",
		strings.NewReader("min_present=8&team_size=notanumber&weekend_counts=false"))
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
		settings:         absence.Settings{TeamSize: 15, MinPresent: 8},
		updateSettingErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/admin/settings",
		"application/x-www-form-urlencoded",
		strings.NewReader("min_present=8&team_size=15&weekend_counts=false"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

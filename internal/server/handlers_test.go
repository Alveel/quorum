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

	"github.com/alveel/vacation-coverage/internal/config"
	"github.com/alveel/vacation-coverage/internal/vacation"
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

// --- createVacation ---

func TestCreateVacation_InvalidDates_Returns422(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: vacation.Settings{TeamSize: 15, MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/vacations",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=notadate&end_date=2026-07-14"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
}

func TestCreateVacation_ThresholdDenied_Returns422(t *testing.T) {
	// 14 on vacation Jul 7 → 1 present; adding requester → 0 < min=8.
	jul7 := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	st := &fakeStore{
		settings: vacation.Settings{TeamSize: 15, MinPresent: 8, WeekendCounts: true},
		perDay:   map[time.Time]int{jul7: 14},
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/vacations",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-07&end_date=2026-07-07"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Coverage would drop below minimum") {
		t.Errorf("body missing denial message: %s", body)
	}
}

func TestCreateVacation_Success_Returns200WithOOBSwaps(t *testing.T) {
	st := &fakeStore{
		settings: vacation.Settings{TeamSize: 15, MinPresent: 8},
		createVac: vacation.Vacation{
			UserID:    "testuser",
			StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			EndDate:   time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
			Status:    vacation.StatusApproved,
		},
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/vacations",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-01&end_date=2026-07-05"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
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
	if !strings.Contains(bodyStr, `id="my-vacations"`) {
		t.Error("response missing my-vacations section")
	}
}

func TestCreateVacation_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings:     vacation.Settings{TeamSize: 15, MinPresent: 8},
		createVacErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/vacations",
		"application/x-www-form-urlencoded",
		strings.NewReader("start_date=2026-07-01&end_date=2026-07-05"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

// --- cancelVacation ---

func TestCancelVacation_InvalidUUID_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: vacation.Settings{TeamSize: 15, MinPresent: 8}})
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/vacations/not-a-uuid", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestCancelVacation_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings:  vacation.Settings{TeamSize: 15, MinPresent: 8},
		cancelErr: errors.New("not found or not cancellable"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/vacations/00000000-0000-0000-0000-000000000001", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

func TestCancelVacation_Success_RendersFragments(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: vacation.Settings{TeamSize: 15, MinPresent: 8}})
	defer ts.Close()

	req, _ := http.NewRequest("DELETE", ts.URL+"/vacations/00000000-0000-0000-0000-000000000001", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `id="my-vacations"`) {
		t.Error("response missing my-vacations section")
	}
	if !strings.Contains(bodyStr, `id="heatmap"`) {
		t.Error("response missing heatmap OOB swap")
	}
}

// --- dayDetail ---

func TestDayDetail_BadDate_Returns400(t *testing.T) {
	ts := newTestServer(&fakeStore{settings: vacation.Settings{TeamSize: 15, MinPresent: 8}})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/not-a-date")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

func TestDayDetail_Success_RendersPanel(t *testing.T) {
	st := &fakeStore{
		settings: vacation.Settings{TeamSize: 15, MinPresent: 8},
		onDay: []vacation.Vacation{
			{
				UserName:  "Alice",
				StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
				EndDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
				Status:    vacation.StatusApproved,
			},
		},
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/2026-07-05")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "day-detail-panel") {
		t.Errorf("response missing day-detail-panel: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Alice") {
		t.Error("response missing vacation owner name")
	}
}

func TestDayDetail_StoreError_Returns500(t *testing.T) {
	st := &fakeStore{
		settings: vacation.Settings{TeamSize: 15, MinPresent: 8},
		onDayErr: errors.New("db error"),
	}
	ts := newTestServer(st)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/day/2026-07-05")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("want 500, got %d", resp.StatusCode)
	}
}

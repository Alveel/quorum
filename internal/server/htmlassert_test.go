package server

import (
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// dayCell is a day square parsed out of a rendered heatmap.
type dayCell struct {
	date    string
	classes map[string]bool
	title   string
}

func (c dayCell) hasClass(name string) bool { return c.classes[name] }

func (c dayCell) classList() []string {
	out := make([]string, 0, len(c.classes))
	for k := range c.classes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// parseDayCells extracts every day square carrying a data-date, keyed by that date.
// A duplicate date fails the test rather than resolving to whichever came last: a
// response holding both a page fragment and its out-of-band copy would otherwise be
// indistinguishable from one holding a single correct heatmap.
func parseDayCells(t *testing.T, body string) map[string]dayCell {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse response body: %v", err)
	}

	cells := map[string]dayCell{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := make(map[string]string, len(n.Attr))
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if date := attrs["data-date"]; date != "" {
				if _, dup := cells[date]; dup {
					t.Fatalf("day %s rendered more than once", date)
				}
				classes := map[string]bool{}
				for _, c := range strings.Fields(attrs["class"]) {
					classes[c] = true
				}
				cells[date] = dayCell{date: date, classes: classes, title: attrs["title"]}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return cells
}

// requireDayCell returns the parsed cell for date, failing if the heatmap omitted it.
func requireDayCell(t *testing.T, body, date string) dayCell {
	t.Helper()
	cells := parseDayCells(t, body)
	c, ok := cells[date]
	if !ok {
		t.Fatalf("heatmap has no cell for %s (%d cells rendered)", date, len(cells))
	}
	return c
}

// assertShortDayRendered asserts that the response's heatmap was built from the full
// set of coverage inputs, by checking the one day shortDayStore leaves below the
// minimum. Both ways of losing those inputs change this cell:
//
//   - a roster, role or holiday load that failed leaves Expected at 0, so the day
//     renders "none" rather than red;
//   - a settings load that failed drops MinPresent to 0, so 7 of 15 present renders
//     orange rather than red.
//
// Asserting only that a heatmap element exists cannot see either.
func assertShortDayRendered(t *testing.T, body string) {
	t.Helper()
	c := requireDayCell(t, body, shortDay)
	if !c.hasClass("red") {
		t.Errorf("day %s: want class red, got %v (title %q)", shortDay, c.classList(), c.title)
	}
	// Match the count only: the surrounding date format is incidental, and the
	// neighbouring legend text is already localized.
	if want := "7/15 present"; !strings.Contains(c.title, want) {
		t.Errorf("day %s: want title containing %q, got %q", shortDay, want, c.title)
	}
}

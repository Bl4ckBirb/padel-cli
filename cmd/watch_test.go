package cmd

import "testing"

func slotResult(clubID, clubName, date string, slots ...AvailabilitySlot) []SearchResult {
	return []SearchResult{{
		Date: date,
		Clubs: []SearchClubResult{{
			ClubID:   clubID,
			ClubName: clubName,
			Slots:    slots,
		}},
	}}
}

func TestCollectNewSlots(t *testing.T) {
	seen := map[string]struct{}{}
	a := AvailabilitySlot{Court: "Court 1", Time: "18:00", Duration: 90}
	b := AvailabilitySlot{Court: "Court 2", Time: "19:30", Duration: 90}

	// First poll: both slots are new.
	fresh := collectNewSlots(slotResult("c1", "Club", "2026-06-20", a, b), seen, 0)
	if len(fresh) != 2 {
		t.Fatalf("first poll: want 2 new slots, got %d", len(fresh))
	}
	if len(seen) != 2 {
		t.Fatalf("first poll: want seen size 2, got %d", len(seen))
	}

	// Second poll, identical: nothing new.
	fresh = collectNewSlots(slotResult("c1", "Club", "2026-06-20", a, b), seen, 0)
	if len(fresh) != 0 {
		t.Fatalf("second poll: want 0 new slots, got %d", len(fresh))
	}

	// Slot b vanishes (only a present): nothing new, b dropped from seen.
	fresh = collectNewSlots(slotResult("c1", "Club", "2026-06-20", a), seen, 0)
	if len(fresh) != 0 {
		t.Fatalf("vanish poll: want 0 new slots, got %d", len(fresh))
	}
	if len(seen) != 1 {
		t.Fatalf("vanish poll: want seen size 1, got %d", len(seen))
	}

	// b reappears: it should re-alert.
	fresh = collectNewSlots(slotResult("c1", "Club", "2026-06-20", a, b), seen, 0)
	if len(fresh) != 1 || fresh[0].Slot.Court != "Court 2" {
		t.Fatalf("reappear poll: want 1 new slot (Court 2), got %d", len(fresh))
	}
}

func TestCollectNewSlotsDurationFilter(t *testing.T) {
	seen := map[string]struct{}{}
	short := AvailabilitySlot{Court: "Court 1", Time: "18:00", Duration: 90}
	long := AvailabilitySlot{Court: "Court 1", Time: "18:00", Duration: 120}

	fresh := collectNewSlots(slotResult("c1", "Club", "2026-06-20", short, long), seen, 120)
	if len(fresh) != 1 || fresh[0].Slot.Duration != 120 {
		t.Fatalf("want only the 120-min slot, got %d slots", len(fresh))
	}
	if len(seen) != 1 {
		t.Fatalf("filtered slot must not enter seen-set: size %d", len(seen))
	}
}

func TestFormatWatchAlert(t *testing.T) {
	slots := []watchSlot{{
		ClubID:   "c1",
		ClubName: "My Club",
		Date:     "2026-06-20",
		Slot:     AvailabilitySlot{Court: "Court 1", Time: "18:00", Duration: 90, Price: "20 EUR"},
	}}
	msg := formatWatchAlert(slots)
	for _, want := range []string{"My Club", "Court 1", "18:00", "90 min", "20 EUR", "app.playtomic.io/clubs/c1"} {
		if !contains(msg, want) {
			t.Errorf("alert missing %q in:\n%s", want, msg)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

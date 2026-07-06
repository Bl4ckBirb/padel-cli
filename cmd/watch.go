package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"padel-cli/storage"

	"github.com/spf13/cobra"
)

const watchStateFile = "watch-state.json"

func watchCmd() *cobra.Command {
	var location string
	var clubID string
	var venuesInput string
	var date string
	var timeRange string
	var weekend bool
	var radius int
	var duration int
	var showIndoor bool
	var showOutdoor bool
	var interval time.Duration
	var once bool
	var telegramEnabled bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a timeslot for openings and alert on Telegram",
		Long: `Poll a venue/date/time window and send a Telegram alert whenever a slot
opens up (for example when someone cancels). Notify-only — it never books.

Targeting mirrors 'padel search': pick venues with --venues, --club-id, or
--location, then narrow with --date, --time, --duration and the indoor/outdoor
flags. By default it loops on --interval; use --once for Task Scheduler/cron.

Telegram credentials are read from the TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID
environment variables. If they are missing, alerts are printed to the console.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clubID != "" && venuesInput != "" {
				return fmt.Errorf("use either --club-id or --venues, not both")
			}
			if showIndoor && showOutdoor {
				return fmt.Errorf("use either --indoor or --outdoor, not both")
			}
			if clubID == "" && venuesInput == "" {
				if location == "" {
					location = cfg.DefaultLocation
				}
				if location == "" {
					return fmt.Errorf("--location is required (or set default_location in config)")
				}
			}
			if interval <= 0 {
				return fmt.Errorf("--interval must be positive")
			}

			var dateInputs []string
			if weekend {
				for _, d := range nextWeekendDates(time.Now()) {
					dateInputs = append(dateInputs, d.Format("2006-01-02"))
				}
			} else {
				if date == "" {
					return fmt.Errorf("--date is required unless --weekend is set")
				}
				for _, part := range strings.Split(date, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					parsed, err := parseDateInput(part)
					if err != nil {
						return err
					}
					dateInputs = append(dateInputs, parsed.Format("2006-01-02"))
				}
			}

			var startMinutes, endMinutes int
			var hasTimeRange bool
			if timeRange != "" {
				parsedStart, parsedEnd, err := parseTimeRange(timeRange)
				if err != nil {
					return err
				}
				startMinutes, endMinutes, hasTimeRange = parsedStart, parsedEnd, true
			}

			query := slotQuery{
				clubID:       clubID,
				venuesInput:  venuesInput,
				location:     location,
				radius:       radius,
				dateInputs:   dateInputs,
				startMinutes: startMinutes,
				endMinutes:   endMinutes,
				hasTimeRange: hasTimeRange,
				showIndoor:   showIndoor,
				showOutdoor:  showOutdoor,
			}

			// Best-effort Telegram: fall back to console-only if unconfigured.
			var notifier Notifier = noopNotifier{}
			if telegramEnabled {
				n, err := newNotifierFromFile(true)
				if err != nil {
					return err
				}
				notifier = n
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// Keys already alerted on. For --once we persist across runs so a
			// scheduler doesn't re-alert the same slot; the loop keeps it in memory.
			seen := map[string]struct{}{}
			if once {
				seen = loadWatchState()
			}

			poll := func() error {
				results, err := fetchMatchingSlots(ctx, query)
				if err != nil {
					return err
				}
				fresh := collectNewSlots(results, seen, duration)
				if len(fresh) == 0 {
					fmt.Printf("[%s] no new slots\n", time.Now().Format("15:04:05"))
					return nil
				}
				msg := formatWatchAlert(fresh)
				fmt.Println(msg)
				if err := notifier.Notify(ctx, msg); err != nil {
					fmt.Fprintf(os.Stderr, "warning: notification failed: %v\n", err)
				}
				return nil
			}

			if once {
				if err := poll(); err != nil {
					return err
				}
				return saveWatchState(seen)
			}

			fmt.Printf("Watching with ~%s interval (+/-20%% jitter) — Ctrl-C to stop.\n", interval)
			if err := poll(); err != nil {
				return err
			}
			for {
				// Jitter ±20% of the base interval so polls don't land on a
				// robotic fixed cadence (e.g. always exactly 3m00s apart).
				jitter := time.Duration(float64(interval) * (0.8 + 0.4*rand.Float64()))
				select {
				case <-ctx.Done():
					fmt.Println("\nStopped.")
					return nil
				case <-time.After(jitter):
					// A transient API hiccup shouldn't kill a long-running watch.
					if err := poll(); err != nil {
						fmt.Fprintf(os.Stderr, "[%s] poll error: %v\n", time.Now().Format("15:04:05"), err)
					}
				}
			}
		},
	}

	cmd.Flags().StringVar(&location, "location", "", "Location name or lat,lon")
	cmd.Flags().StringVar(&clubID, "club-id", "", "Club (tenant) ID")
	cmd.Flags().StringVar(&venuesInput, "venues", "", "Comma-separated saved venue aliases")
	cmd.Flags().StringVar(&date, "date", "", "Date(s) to watch (YYYY-MM-DD, comma-separated)")
	cmd.Flags().StringVar(&timeRange, "time", "", "Time range (HH:MM-HH:MM)")
	cmd.Flags().BoolVar(&weekend, "weekend", false, "Watch the next Saturday and Sunday")
	cmd.Flags().IntVar(&radius, "radius", 50000, "Search radius in meters")
	cmd.Flags().IntVar(&duration, "duration", 0, "Only alert on slots of this duration in minutes (0 = any)")
	cmd.Flags().BoolVar(&showIndoor, "indoor", false, "Watch only indoor courts")
	cmd.Flags().BoolVar(&showOutdoor, "outdoor", false, "Watch only outdoor courts")
	cmd.Flags().DurationVar(&interval, "interval", 3*time.Minute, "Base polling interval for the built-in loop (actual cadence varies ±20%)")
	cmd.Flags().BoolVar(&once, "once", false, "Poll once and exit (for Task Scheduler/cron)")
	cmd.Flags().BoolVar(&telegramEnabled, "telegram", true, "Send alerts via Telegram (env TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID)")
	return cmd
}

// watchSlot is a single newly-available slot tagged with its club and date.
type watchSlot struct {
	ClubID   string
	ClubName string
	Date     string
	Slot     AvailabilitySlot
}

func slotKey(clubID, date string, s AvailabilitySlot) string {
	return strings.Join([]string{clubID, date, s.Court, s.Time, strconv.Itoa(s.Duration)}, "|")
}

// collectNewSlots diffs the current results against the seen-set and returns the
// slots not previously alerted on. It rebuilds seen to exactly the current set,
// so a slot that disappears is forgotten and re-alerts if it comes back.
func collectNewSlots(results []SearchResult, seen map[string]struct{}, durationFilter int) []watchSlot {
	current := map[string]struct{}{}
	var fresh []watchSlot
	for _, result := range results {
		for _, club := range result.Clubs {
			for _, slot := range club.Slots {
				if durationFilter > 0 && slot.Duration != durationFilter {
					continue
				}
				key := slotKey(club.ClubID, result.Date, slot)
				current[key] = struct{}{}
				if _, ok := seen[key]; ok {
					continue
				}
				fresh = append(fresh, watchSlot{
					ClubID:   club.ClubID,
					ClubName: club.ClubName,
					Date:     result.Date,
					Slot:     slot,
				})
			}
		}
	}
	for k := range seen {
		delete(seen, k)
	}
	for k := range current {
		seen[k] = struct{}{}
	}
	return fresh
}

func formatWatchAlert(slots []watchSlot) string {
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Date != slots[j].Date {
			return slots[i].Date < slots[j].Date
		}
		if slots[i].ClubName != slots[j].ClubName {
			return slots[i].ClubName < slots[j].ClubName
		}
		if slots[i].Slot.Time != slots[j].Slot.Time {
			return slots[i].Slot.Time < slots[j].Slot.Time
		}
		return slots[i].Slot.Court < slots[j].Slot.Court
	})

	var b strings.Builder
	fmt.Fprintf(&b, "🎾 Padel slot opened (%d):\n", len(slots))
	for _, ws := range slots {
		fmt.Fprintf(&b, "• %s %s — %s (%s, %d min", ws.Date, ws.Slot.Time, ws.ClubName, ws.Slot.Court, ws.Slot.Duration)
		if strings.TrimSpace(ws.Slot.Price) != "" {
			fmt.Fprintf(&b, ", %s", ws.Slot.Price)
		}
		b.WriteString(")\n")
		fmt.Fprintf(&b, "  https://app.playtomic.io/clubs/%s\n", ws.ClubID)
	}
	return strings.TrimRight(b.String(), "\n")
}

func watchStatePath() (string, error) {
	dir, err := storage.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, watchStateFile), nil
}

// loadWatchState reads the persisted alert keys. Any error (missing file,
// unreadable, corrupt) is treated as an empty state rather than a hard failure.
func loadWatchState() map[string]struct{} {
	seen := map[string]struct{}{}
	path, err := watchStatePath()
	if err != nil {
		return seen
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return seen
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err != nil {
		return seen
	}
	for _, k := range keys {
		seen[k] = struct{}{}
	}
	return seen
}

func saveWatchState(seen map[string]struct{}) error {
	path, err := watchStatePath()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/j-f-allison/jacktasks/internal/paths"
	"github.com/j-f-allison/jacktasks/internal/session"
	"github.com/j-f-allison/jacktasks/internal/store"
)

func main() {
	ctx := context.Background()

	dbPath, err := paths.DBPath()
	if err != nil {
		log.Fatalf("paths: %v", err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	deviceID, err := s.DeviceID(ctx)
	if err != nil {
		log.Fatalf("device id: %v", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	driver := &driver{store: s, deviceID: deviceID, scanner: scanner, ctx: ctx}
	driver.run()
}

type driver struct {
	store    *store.Store
	deviceID string
	scanner  *bufio.Scanner
	ctx      context.Context
}

func (d *driver) run() {
	m := &session.Machine{}

	// Check for a resume candidate before starting setup.
	d.maybeOfferResume(m)

	// If not already active (resume jumped us to Active), start setup flow.
	if m.State() == session.StateIdle {
		_ = m.BeginSetup()
	}

	for {
		switch m.State() {
		case session.StateIdle:
			return

		case session.StateSetupCategory:
			d.doSetupCategory(m)

		case session.StateSetupProject:
			d.doSetupProject(m)

		case session.StateSetupDuration:
			d.doSetupDuration(m)

		case session.StateActive, session.StatePaused:
			d.doActiveLoop(m)

		case session.StateEndingNotes:
			d.doEndingNotes(m)

		case session.StateWhatNext:
			done := d.doWhatNext(m)
			if done {
				return
			}

		case session.StateBreak:
			d.doBreak(m)
		}
	}
}

// maybeOfferResume checks whether the most recent session ended early and
// offers the user the option to resume it. If accepted, skips setup and
// jumps directly to the duration prompt pre-filled with remaining time.
func (d *driver) maybeOfferResume(m *session.Machine) {
	latest, err := d.store.LatestSession(d.ctx)
	if err != nil || latest.Status != store.SessionEndedEarly {
		return
	}

	remaining := latest.PlannedDurationMin - (latest.ActualDurationSec / 60)
	if remaining <= 0 {
		return
	}

	cat, err := d.store.GetCategory(d.ctx, latest.CategoryID)
	if err != nil {
		return
	}
	proj, err := d.store.GetProject(d.ctx, latest.ProjectID)
	if err != nil {
		return
	}

	fmt.Printf("\nResume %s / %s with %d minutes remaining? [y/N]: ", cat.Name, proj.Name, remaining)
	line := d.readLine()
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		return
	}

	// Jump past category/project setup directly into SetupDuration.
	_ = m.BeginSetup()
	_ = m.SetCategory(latest.CategoryID, time.Now())
	_ = m.SetProject(latest.ProjectID, time.Now())
	now := time.Now()
	if err := m.SetDuration(remaining, now); err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
	}
}

func (d *driver) doSetupCategory(m *session.Machine) {
	cats, err := d.store.ListCategories(d.ctx)
	if err != nil {
		log.Fatalf("list categories: %v", err)
	}

	fmt.Println("\n--- Select category ---")
	if len(cats) == 0 {
		fmt.Println("No categories found. Enter a name to create one:")
		name := d.readLine()
		cat, err := d.store.CreateCategory(d.ctx, strings.TrimSpace(name))
		if err != nil {
			log.Fatalf("create category: %v", err)
		}
		_ = m.SetCategory(cat.ID, time.Now())
		return
	}

	for i, c := range cats {
		fmt.Printf("  %d) %s\n", i+1, c.Name)
	}
	fmt.Printf("  n) New category\n")
	fmt.Print("Choice: ")

	line := strings.TrimSpace(d.readLine())
	if line == "n" || line == "N" {
		fmt.Print("Category name: ")
		name := strings.TrimSpace(d.readLine())
		cat, err := d.store.CreateCategory(d.ctx, name)
		if err != nil {
			log.Fatalf("create category: %v", err)
		}
		_ = m.SetCategory(cat.ID, time.Now())
		return
	}

	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(cats) {
		fmt.Println("Invalid choice, try again.")
		return // loop back
	}
	_ = m.SetCategory(cats[n-1].ID, time.Now())
}

func (d *driver) doSetupProject(m *session.Machine) {
	projs, err := d.store.ListProjectsByCategory(d.ctx, m.CategoryID())
	if err != nil {
		log.Fatalf("list projects: %v", err)
	}

	cat, _ := d.store.GetCategory(d.ctx, m.CategoryID())
	catName := ""
	if cat != nil {
		catName = cat.Name
	}

	fmt.Printf("\n--- Select project (%s) ---\n", catName)
	if len(projs) == 0 {
		fmt.Println("No projects found. Enter a name to create one:")
		name := strings.TrimSpace(d.readLine())
		proj, err := d.store.CreateProject(d.ctx, name, m.CategoryID())
		if err != nil {
			log.Fatalf("create project: %v", err)
		}
		_ = m.SetProject(proj.ID, time.Now())
		return
	}

	for i, p := range projs {
		fmt.Printf("  %d) %s\n", i+1, p.Name)
	}
	fmt.Printf("  n) New project\n")
	fmt.Print("Choice: ")

	line := strings.TrimSpace(d.readLine())
	if line == "n" || line == "N" {
		fmt.Print("Project name: ")
		name := strings.TrimSpace(d.readLine())
		proj, err := d.store.CreateProject(d.ctx, name, m.CategoryID())
		if err != nil {
			log.Fatalf("create project: %v", err)
		}
		_ = m.SetProject(proj.ID, time.Now())
		return
	}

	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(projs) {
		fmt.Println("Invalid choice, try again.")
		return
	}
	_ = m.SetProject(projs[n-1].ID, time.Now())
}

func (d *driver) doSetupDuration(m *session.Machine) {
	fmt.Print("\nPlanned duration (minutes): ")
	line := strings.TrimSpace(d.readLine())
	n, err := strconv.Atoi(line)
	if err != nil || n <= 0 {
		fmt.Println("Enter a positive integer.")
		return
	}
	if err := m.SetDuration(n, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "set duration: %v\n", err)
	}
	fmt.Printf("\nSession started. %d minutes on the clock.\n", n)
	fmt.Println("Commands: upn <text>  ext <n>  pause  resume  end")
}

// doActiveLoop reads one command and dispatches it.
func (d *driver) doActiveLoop(m *session.Machine) {
	now := time.Now()
	rem := m.TimeRemaining(now)

	if m.State() == session.StatePaused {
		fmt.Printf("\n[PAUSED | ~%s remaining] > ", formatDuration(rem))
	} else {
		fmt.Printf("\n[ACTIVE | ~%s remaining] > ", formatDuration(rem))
	}

	line := strings.TrimSpace(d.readLine())
	if line == "" {
		return
	}

	parts := strings.SplitN(line, " ", 2)
	cmd := strings.ToLower(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}

	now = time.Now()
	switch cmd {
	case "upn":
		if arg == "" {
			fmt.Println("Usage: upn <text>")
			return
		}
		if err := m.AddCapture(arg, now); err != nil {
			fmt.Fprintf(os.Stderr, "upn: %v\n", err)
			return
		}
		fmt.Printf("Captured: %q\n", arg)

	case "ext":
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 {
			fmt.Println("Usage: ext <minutes>")
			return
		}
		if err := m.Extend(n, now); err != nil {
			fmt.Fprintf(os.Stderr, "ext: %v\n", err)
			return
		}
		fmt.Printf("Extended by %d minutes.\n", n)

	case "pause":
		if m.State() == session.StatePaused {
			fmt.Println("Already paused. Use 'resume' to continue.")
			return
		}
		if err := m.Pause(now); err != nil {
			fmt.Fprintf(os.Stderr, "pause: %v\n", err)
		}

	case "resume":
		if m.State() == session.StateActive {
			fmt.Println("Session is not paused.")
			return
		}
		if err := m.Resume(now); err != nil {
			fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		}

	case "end":
		if err := m.End(now); err != nil {
			fmt.Fprintf(os.Stderr, "end: %v\n", err)
		}

	default:
		fmt.Printf("Unknown command %q. Commands: upn <text>  ext <n>  pause  resume  end\n", cmd)
	}
}

func (d *driver) doEndingNotes(m *session.Machine) {
	caps := m.Captures()
	if len(caps) > 0 {
		fmt.Println("\n--- Captures from this session ---")
		for _, c := range caps {
			fmt.Printf("  • %s\n", c.Text)
		}
	}
	fmt.Print("\nEnd notes (or press Enter to skip): ")
	notes := strings.TrimSpace(d.readLine())
	_ = m.SetEndNotes(notes, time.Now())

	// Write session row to the store now.
	in, err := m.ToStoreSessionInput(d.deviceID)
	if err != nil {
		log.Fatalf("build session input: %v", err)
	}
	sess, err := d.store.CreateSession(d.ctx, in)
	if err != nil {
		log.Fatalf("save session: %v", err)
	}

	// Write captures.
	for _, c := range caps {
		if _, err := d.store.CreateCapture(d.ctx, sess.ID, c.Text); err != nil {
			fmt.Fprintf(os.Stderr, "save capture: %v\n", err)
		}
	}

	status := "ended early"
	if in.Status == store.SessionCompleted {
		status = "completed"
	}
	fmt.Printf("\nSession saved (%s, %s actual).\n",
		status,
		formatDuration(time.Duration(in.ActualDurationSec)*time.Second),
	)
}

// doWhatNext shows the what-next screen. Returns true if the user chose End.
func (d *driver) doWhatNext(m *session.Machine) bool {
	fmt.Println("\n--- What next? ---")
	fmt.Println("  1) Continue session (same category/project)")
	fmt.Println("  2) New session")
	fmt.Println("  3) Break (5 minutes)")
	fmt.Println("  4) End")
	fmt.Print("Choice: ")

	line := strings.TrimSpace(d.readLine())
	now := time.Now()

	switch line {
	case "1":
		fmt.Print("Duration for next session (minutes): ")
		minStr := strings.TrimSpace(d.readLine())
		n, err := strconv.Atoi(minStr)
		if err != nil || n <= 0 {
			fmt.Println("Enter a positive integer.")
			return false
		}
		if err := m.ContinueSession(n, now); err != nil {
			fmt.Fprintf(os.Stderr, "continue: %v\n", err)
			return false
		}
		fmt.Printf("Session started. %d minutes on the clock.\n", n)
		fmt.Println("Commands: upn <text>  ext <n>  pause  resume  end")

	case "2":
		_ = m.NewSession(now)

	case "3":
		_ = m.StartBreak(now)

	case "4":
		_ = m.Finish(now)
		fmt.Println("Done.")
		return true

	default:
		fmt.Println("Enter 1, 2, 3, or 4.")
	}
	return false
}

func (d *driver) doBreak(m *session.Machine) {
	fmt.Println("\n[BREAK] Press Enter when ready to continue.")
	d.readLine()
	_ = m.EndBreak(time.Now())
}

func (d *driver) readLine() string {
	if d.scanner.Scan() {
		return d.scanner.Text()
	}
	if err := d.scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
	}
	os.Exit(0)
	return ""
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	total := int(d.Seconds())
	m := total / 60
	s := total % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

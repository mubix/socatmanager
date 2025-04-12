package main

import (
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"syscall" // Required for syscall.SIGTERM

	"github.com/google/uuid" // For generating unique IDs
)

// LogEntry defines the structure for an event log record
type LogEntry struct {
	Timestamp time.Time
	EventType string // "Start", "Stop", "Died"
	Details   string
}

var (
	runningProcesses = make(map[string]*SocatProcess)
	processesMutex   sync.RWMutex
	templates        *template.Template
	lastError        string

	// Event Logging
	eventLog      []LogEntry
	logMutex      sync.Mutex // Separate mutex for the log
	maxLogEntries = 100      // Maximum number of log entries to keep
)


// SocatProcess holds information about a running socat instance
type SocatProcess struct {
	ID         string
	BaseIP     string
	BasePort   string
	RemoteIP   string
	RemotePort string
	Cmd        *exec.Cmd
	Process    *os.Process // Store os.Process for easier access to PID and killing
}

func main() {
	// Find and parse the HTML template
	templatePath := filepath.Join("templates", "index.html")
	var err error
	templates, err = template.ParseFiles(templatePath)
	if err != nil {
		log.Fatalf("Error parsing template %s: %v", templatePath, err)
	}

	// Define HTTP handlers
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/start", handleStart)
	http.HandleFunc("/stop", handleStop)

	// Start the web server
	port := "8080"
	log.Printf("Starting socat manager server on http://localhost:%s\n", port)
	err = http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

// addLogEntry adds a new entry to the event log, managing mutex and size cap
func addLogEntry(eventType, details string) {
	logMutex.Lock()
	defer logMutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(), // Use current server time
		EventType: eventType,
		Details:   details,
	}

	// Prepend to keep newest first easily
	eventLog = append([]LogEntry{entry}, eventLog...)

	// Trim log if it exceeds max size
	if len(eventLog) > maxLogEntries {
		eventLog = eventLog[:maxLogEntries] // Keep the first (newest) maxLogEntries
	}
}


// handleIndex displays the main page, checks process status using /proc, and shows logs
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	processesMutex.Lock() // Need write lock as we might remove processes

	idsToRemove := []string{}
	processesToCheck := make([]*SocatProcess, 0, len(runningProcesses))
	for _, proc := range runningProcesses {
		processesToCheck = append(processesToCheck, proc)
	}

	// Check if tracked processes are still alive using /proc filesystem (Linux specific)
	for _, procInfo := range processesToCheck {
		// Construct the path to the process's directory in /proc
		log.Printf("Checking PID: %d", procInfo.Process.Pid)
		procPath := filepath.Join("/proc", strconv.Itoa(procInfo.Process.Pid), "cwd")

		// Attempt to get file info for the directory
		_, err := os.Stat(procPath)

		if err != nil {
			// If the error indicates the directory does not exist, the process is gone
			if os.IsNotExist(err) {
				log.Printf("Detected dead process via /proc check: PID %d (ID: %s). Removing.", procInfo.Process.Pid, procInfo.ID)
				idsToRemove = append(idsToRemove, procInfo.ID)
				details := fmt.Sprintf("PID %d (Base: %s:%s, Remote: %s:%s)",
					procInfo.Process.Pid, procInfo.BaseIP, procInfo.BasePort, procInfo.RemoteIP, procInfo.RemotePort)
				addLogEntry("Died", details) // Log the dead PID event BEFORE removing
			} else {
				// Log other unexpected errors accessing /proc (e.g., permissions)
				log.Printf("Unexpected error checking process PID %d (ID: %s) via /proc: %v", procInfo.Process.Pid, procInfo.ID, err)
				// Decide if you want to remove based on other errors? For now, we only remove on confirmed non-existence.
			}
		}
		// If err is nil, the directory exists, so the process is assumed to be running.
	}

	// Remove dead processes from tracking
	if len(idsToRemove) > 0 {
		for _, id := range idsToRemove {
			delete(runningProcesses, id)
		}
	}

	// Create a slice copy of current processes for the template
	processesList := make([]*SocatProcess, 0, len(runningProcesses))
	for _, proc := range runningProcesses {
		processesList = append(processesList, proc)
	}

	currentError := lastError
	lastError = "" // Clear error after preparing to display

	processesMutex.Unlock() // Unlock process map

	// Get a copy of the event log for the template
	logMutex.Lock()
	logCopy := make([]LogEntry, len(eventLog))
	copy(logCopy, eventLog) // Copy entries
	logMutex.Unlock()

	// Prepare data for the template
	data := struct {
		Processes []*SocatProcess
		EventLog  []LogEntry
		Error     string
	}{
		Processes: processesList,
		EventLog:  logCopy,
		Error:     currentError,
	}

	// Execute the template
	err := templates.Execute(w, data)
	if err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}



// handleStart starts a new socat process with input validation
func handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form values
	if err := r.ParseForm(); err != nil {
		log.Printf("Error parsing form: %v", err)
		setErrorAndRedirect(w, r, "Failed to parse form data.")
		return
	}
	baseIP := r.FormValue("baseIP")
	basePortStr := r.FormValue("basePort")
	remoteIP := r.FormValue("remoteIP")
	remotePortStr := r.FormValue("remotePort")

	// Input Validation, because command injection bad!

	// Validate Base IP
	if net.ParseIP(baseIP) == nil {
		setErrorAndRedirect(w, r, fmt.Sprintf("Invalid Base IP address format: %s", baseIP))
		return
	}

	// Validate Base Port
	basePort, err := strconv.Atoi(basePortStr)
	if err != nil {
		setErrorAndRedirect(w, r, fmt.Sprintf("Base Port must be a number: %s", basePortStr))
		return
	}
	if basePort < 1 || basePort > 65535 {
		setErrorAndRedirect(w, r, fmt.Sprintf("Base Port must be between 1 and 65535, got: %d", basePort))
		return
	}

	// Validate Remote IP
	if net.ParseIP(remoteIP) == nil {
		setErrorAndRedirect(w, r, fmt.Sprintf("Invalid Remote IP address format: %s", remoteIP))
		return
	}

	// Validate Remote Port
	remotePort, err := strconv.Atoi(remotePortStr)
	if err != nil {
		setErrorAndRedirect(w, r, fmt.Sprintf("Remote Port must be a number: %s", remotePortStr))
		return
	}
	if remotePort < 1 || remotePort > 65535 {
		setErrorAndRedirect(w, r, fmt.Sprintf("Remote Port must be between 1 and 65535, got: %d", remotePort))
		return
	}

	// Construct the socat command (using the validated string representations of ports)
	// Format: socat tcp4-listen:<base_port>,reuseaddr,bind=<base_ip>,fork tcp4:<remote_ip>:<remote_port>
	listenAddr := fmt.Sprintf("tcp4-listen:%s,reuseaddr,bind=%s,fork", basePortStr, baseIP) // Use original port string
	remoteAddr := fmt.Sprintf("tcp4:%s:%s", remoteIP, remotePortStr)                     // Use original port string
	cmd := exec.Command("socat", listenAddr, remoteAddr)

	// Start the command in the background
	err = cmd.Start()
	if err != nil {
		log.Printf("Error starting socat process: %v", err)
		// Check if socat command was not found
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			setErrorAndRedirect(w, r, "Failed to start socat: 'socat' command not found in PATH. Please ensure socat is installed.")
		} else {
			// Provide more context on socat errors (e.g., binding issues)
			// Note: Capturing stderr from socat would give even better diagnostics, but adds complexity.
			setErrorAndRedirect(w, r, fmt.Sprintf("Failed to start socat (check if address/port is already in use or socat permissions): %v", err))
		}
		return
	}

	// Generate a unique ID for tracking
	id := uuid.New().String()

	// Store the process information
	procInfo := &SocatProcess{
		ID:         id,
		BaseIP:     baseIP,
		BasePort:   basePortStr, // Store the string version
		RemoteIP:   remoteIP,
		RemotePort: remotePortStr, // Store the string version
		Cmd:        cmd,
		Process:    cmd.Process, // Store os.Process
	}

	processesMutex.Lock() // Use write lock for modifying the map
	runningProcesses[id] = procInfo
	processesMutex.Unlock()

	log.Printf("Started socat forward (ID: %s): PID %d - %s -> %s", id, cmd.Process.Pid, listenAddr, remoteAddr)

	details := fmt.Sprintf("PID %d (Base: %s:%s, Remote: %s:%s)",
		cmd.Process.Pid, baseIP, basePortStr, remoteIP, remotePortStr)
	addLogEntry("Start", details)

	// Redirect back to the index page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}


// handleStop stops a running socat process
func handleStop(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		setErrorAndRedirect(w, r, "No process ID specified.")
		return
	}

	processesMutex.Lock() // Use write lock to modify the map
	procInfo, found := runningProcesses[id]
	if !found {
		processesMutex.Unlock()
		setErrorAndRedirect(w, r, fmt.Sprintf("Process with ID %s not found.", id))
		return
	}

	// Log the stop event BEFORE removing from map/killing
	details := fmt.Sprintf("PID %d (Base: %s:%s, Remote: %s:%s)",
		procInfo.Process.Pid, procInfo.BaseIP, procInfo.BasePort, procInfo.RemoteIP, procInfo.RemotePort)
	addLogEntry("Stop", details) // Log user-initiated stop

	// Remove from map *before* trying to kill, to prevent race conditions
	// if multiple stop requests arrive for the same ID
	delete(runningProcesses, id)
	processesMutex.Unlock()

	// Attempt to terminate the process gracefully first, then kill if necessary
	log.Printf("Stopping socat forward (ID: %s): PID %d", procInfo.ID, procInfo.Process.Pid)

	// Send SIGTERM first (more graceful shutdown)
	err := procInfo.Process.Signal(syscall.SIGTERM)
	if err != nil {
		log.Printf("Failed to send SIGTERM to process %d (ID: %s): %v. Attempting SIGKILL.", procInfo.Process.Pid, procInfo.ID, err)
		// If SIGTERM fails (e.g., process already dead), try SIGKILL
		errKill := procInfo.Process.Kill()
		if errKill != nil {
			log.Printf("Failed to send SIGKILL to process %d (ID: %s): %v", procInfo.Process.Pid, procInfo.ID, errKill)
			// Don't set error message for redirect, as process is likely already gone
		} else {
			log.Printf("Successfully sent SIGKILL to process %d (ID: %s).", procInfo.Process.Pid, procInfo.ID)
		}
	} else {
		log.Printf("Successfully sent SIGTERM to process %d (ID: %s).", procInfo.Process.Pid, procInfo.ID)
	}

	// Wait for the process to actually exit (optional but good practice)
	// Note: This might block the handler briefly. Could be moved to a goroutine if needed.
	_, waitErr := procInfo.Process.Wait()
	if waitErr != nil && waitErr.Error() != "wait: no child processes" && waitErr.Error() != "exit status -1" { // Ignore common errors for already-killed processes
		log.Printf("Error waiting for process %d (ID: %s) to exit: %v", procInfo.Process.Pid, procInfo.ID, waitErr)
	} else {
	    log.Printf("Process %d (ID: %s) confirmed exited.", procInfo.Process.Pid, procInfo.ID)
	}


	// Redirect back to the index page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Helper function to set an error message and redirect
func setErrorAndRedirect(w http.ResponseWriter, r *http.Request, errorMsg string) {
	processesMutex.Lock()
	lastError = errorMsg
	processesMutex.Unlock()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}


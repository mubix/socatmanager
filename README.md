# socatmanager
Simple Socat Manager Web UI

# Socat Web Manager

A simple web application written in Go to manage `socat` TCP forwards through a user-friendly web interface. Start, stop, and monitor multiple `socat` listeners/forwards without needing complex command-line management.

## Overview

This tool provides a web UI to dynamically create TCP forwards using `socat`. You provide a local IP and port for `socat` to listen on, and a remote IP and port for `socat` to forward traffic to. The application manages these `socat` processes, allows you to stop them individually, and keeps track of their status.

It's particularly useful for temporarily forwarding ports during development, testing, or network troubleshooting scenarios.

## Features

* **Web Interface:** Clean UI for managing forwards.
* **Start Forwards:** Easily start new `socat tcp4-listen <...> tcp4:<...>` forwards.
* **Input Validation:** Validates IP address formats and port ranges (1-65535) before attempting to start `socat`.
* **List Active Forwards:** Displays a table of currently running forwards managed by the application, including PIDs.
* **Stop Forwards:** Stop individual `socat` processes with a button click.
* **Process Monitoring (Linux):** Automatically checks if managed `socat` processes are still running using the `/proc` filesystem. Dead processes are removed from the list.
* **Event Log:** Displays a running log of events:
    * `Start`: A new forward was successfully started.
    * `Stop`: A forward was manually stopped via the UI.
    * `Dead`: A previously running forward's process was detected as no longer running.
* **In-Memory State:** Process list and event log are stored in memory (cleared on application restart).
* **Single Binary:** Compiles to a single executable for easy deployment.

## Prerequisites

1.  **Go:** Version 1.18 or later installed (https://go.dev/doc/install).
2.  **Socat:** The `socat` utility must be installed on the host machine and accessible via the system's `PATH`. You can usually install it via your package manager (e.g., `sudo apt install socat` or `sudo yum install socat`).
3.  **Operating System:** **Linux** is required for the automatic dead process detection feature, which relies on the `/proc` filesystem. The core functionality *might* work on other Unix-like systems, but process monitoring may fail.

## Installation

1.  **Clone the repository:**
    ```bash
    git clone github.com/mubix/socatmanager
    cd socatmanager
    ```
2.  **Initialize Go Modules (if needed):**
    If you haven't already, initialize Go modules in the project directory:
    ```bash
    go mod init mubix.com/socatmanager
    ```
3.  **Fetch Dependencies:**
    The application uses `github.com/google/uuid`. Fetch it:
    ```bash
    go mod tidy
    ```

## Building

Compile the application into a single binary:

```bash
go build -o socatmanager main.go
# Or simply:
# go build
```

This will create an executable file named `socatmanager` in the current directory.

## Running

1.  **Execute the binary:**
    ```bash
    ./socatmanager
    ```

2.  **Access the Web UI:**
    Open your web browser and navigate to:
    `http://localhost:8080` (or the appropriate hostname/IP if running remotely).

The server will start logging output to your console.

* **Running in the Background (Optional):** For longer-term use, you might want to run it using `nohup`, `screen`, `tmux`, or set it up as a `systemd` service:
    ```bash
    nohup ./socat-manager &
    ```

## Usage

1.  **Navigate** to `http://localhost:8080` or the IP of the server you are running it on.
2.  **Start a Forward:**
    * Fill in the "Base IP" (IP address for `socat` to bind to, e.g., `0.0.0.0` or `192.168.1.100`). **This needs to be an IP address that the sever can actually bind to.**
    * Fill in the "Base Port" (Port for `socat` to listen on, e.g., `4444`).
    * Fill in the "Remote IP" (Target IP address to forward traffic to).
    * Fill in the "Remote Port" (Target port to forward traffic to).
    * Click "Start Socat Forward".
    * If successful, the page will refresh, and the new forward will appear in the "Running Forwards" table. Errors (validation or `socat` startup) will be displayed at the top.
3.  **View Forwards:** The "Running Forwards" table shows details (Base/Remote IPs & Ports, PID) for each active `socat` process managed by this tool.
4.  **Stop a Forward:** Click the "Stop" button next to the desired forward in the table. The page will refresh, the process will be terminated, and a "Stop" event will appear in the log.
5.  **Monitor Events:** The "Event Log" section shows a timestamped history of Start, Stop, and Missing events for the current application session.

## Configuration

* **Port:** The web server listens on port `8080` by default. This is currently hardcoded in `main.go`. To change it, modify the `port` variable in the `main` function and rebuild.

## Troubleshooting

* **Error: "socat command not found"**: Ensure `socat` is installed correctly and its location is included in your system's `PATH` environment variable for the user running `socat-manager`.
* **Error: "Failed to start socat (check if address/port is already in use...)"**: The specified "Base IP" and "Base Port" combination is likely already being used by another application (or another `socat` instance). Choose a different Base Port or stop the conflicting process. It could also indicate a permissions issue preventing `socat` from binding to the requested address/port.
* **Forwards marked "Dead" unexpectedly:** This means the application checked `/proc/<PID>/cwd` and found the process was gone. Check system logs (`dmesg`, `/var/log/syslog`, etc.) or `socat`'s behavior if it's crashing. This can happen is the `socat` process is killed, segfaults, or fails to start (bad Base IP).
* **Application doesn't start / Port conflict**: Ensure port `8080` (or the configured port) is not already in use by another service.

## License

This project is licensed under the [BSD 3-clause License](LICENSE.md).

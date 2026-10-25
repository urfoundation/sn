// Package winservice keeps one user's miner running in the background on
// Windows, the way the systemd user service and the launchd agent do on
// Linux and macOS, without administrator rights.
//
// Windows lets a standard user start a program at sign-in only from the
// Startup folder (or the Run key); a logon task or a real service needs an
// administrator. So install writes a minimized Startup shortcut that runs
// "provider service run <name>". That instance has a console window, so it
// starts a copy of itself without one and exits; the copy is the supervisor.
//
// The supervisor does what the systemd unit and its timer do:
//
//   - it runs "provider start" with no window, its output appended to
//     service.log in the state folder, inside a job object that ends the miner
//     if the supervisor dies;
//   - it starts the miner again 15 seconds after it fails, except after exit
//     code 3 (the device needs setup), as Restart=on-failure with
//     RestartPreventExitStatus=3 does;
//   - it runs "provider update --auto" 15 minutes after it starts and every
//     6 hours (plus up to an hour at random) after that, outside the job, so
//     an update can restart the supervisor it came from.
//
// A supervisor holds a lock on <name>.lock in the service folder while it
// runs; that lock is what "running" means. Stop sets a named event the
// supervisor waits on; it passes the stop on to the miner through an event
// handle the miner inherited (URNETWORK_SERVICE_STOP_HANDLE), so the miner
// ends as it does on Ctrl-C.
//
// Only the Windows build has an implementation; the package is empty
// elsewhere.
package winservice

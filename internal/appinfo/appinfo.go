// Package appinfo provides application identity constants.
// These are used across packages for consistent naming.
package appinfo

const (
	// AppName is the display name of the application.
	AppName = "VRClog Companion"

	// DirName is the directory name used for storing application data.
	// Location: %LOCALAPPDATA%/vrclog/ (Windows) or ~/.config/vrclog/ (other)
	DirName = "vrclog"

	// MutexName is the Windows mutex name for single instance control.
	// "Local\" prefix means the mutex is scoped to the current user session,
	// not system-wide. This is appropriate for desktop applications.
	MutexName = "Local\\vrclog-companion"

	// LockFileName is the lock file name for single instance control.
	LockFileName = "vrclog.lock"

	// ConfigFileName is the configuration file name.
	ConfigFileName = "config.json"

	// SecretsFileName is the secrets file name.
	SecretsFileName = "secrets.json"

	// DatabaseFileName is the SQLite database file name.
	DatabaseFileName = "vrclog.sqlite"
)

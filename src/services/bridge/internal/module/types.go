package module

// ModuleStatus represents the runtime state of a module.
type ModuleStatus string

const (
	StatusNotInstalled          ModuleStatus = "not_installed"
	StatusInstalledDisconnected ModuleStatus = "installed_disconnected"
	StatusPendingBootstrap      ModuleStatus = "pending_bootstrap"
	StatusRunning               ModuleStatus = "running"
	StatusStopped               ModuleStatus = "stopped"
	StatusError                 ModuleStatus = "error"
)

// ModuleCategory is the frontend display category.
type ModuleCategory string

const (
	CategoryMemory         ModuleCategory = "memory"
	CategorySandbox        ModuleCategory = "sandbox"
	CategorySearch         ModuleCategory = "search"
	CategoryBrowser        ModuleCategory = "browser"
	CategoryConsole        ModuleCategory = "console"
	CategoryInfrastructure ModuleCategory = "infrastructure"
)

// ModuleCapabilities describes what operations a module supports.
type ModuleCapabilities struct {
	Installable            bool `json:"installable" yaml:"installable"`
	Configurable           bool `json:"configurable" yaml:"configurable"`
	Healthcheck            bool `json:"healthcheck" yaml:"healthcheck"`
	BootstrapSupported     bool `json:"bootstrap_supported" yaml:"bootstrap_supported"`
	ExternalAdminSupported bool `json:"external_admin_supported" yaml:"external_admin_supported"`
	PrivilegedRequired     bool `json:"privileged_required" yaml:"privileged_required"`
}

// ModuleInfo is the API response type matching the frontend contract.
type ModuleInfo struct {
	ID                string             `json:"id"`
	Name              string             `json:"name"`
	Description       string             `json:"description"`
	Category          ModuleCategory     `json:"category"`
	Status            ModuleStatus       `json:"status"`
	Version           string             `json:"version,omitempty"`
	Port              *int               `json:"port,omitempty"`
	WebURL            string             `json:"web_url,omitempty"`
	Capabilities      ModuleCapabilities `json:"capabilities"`
	DependsOn         []string           `json:"depends_on"`
	MutuallyExclusive []string           `json:"mutually_exclusive"`
}

// ModuleAction represents an action that can be performed on a module.
type ModuleAction string

const (
	ActionInstall             ModuleAction = "install"
	ActionStart               ModuleAction = "start"
	ActionStop                ModuleAction = "stop"
	ActionRestart             ModuleAction = "restart"
	ActionConfigure           ModuleAction = "configure"
	ActionConfigureConnection ModuleAction = "configure_connection"
	ActionBootstrapDefaults   ModuleAction = "bootstrap_defaults"
)

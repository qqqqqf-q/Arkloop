package formatter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type PluginView struct {
	ID          string          `json:"id"`
	Version     string          `json:"version"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description,omitempty"`
	SourceKind  string          `json:"source_kind"`
	SourceURI   string          `json:"source_uri,omitempty"`
	IsActive    bool            `json:"is_active"`
	Manifest    json.RawMessage `json:"manifest,omitempty"`
}

type PluginEnablementView struct {
	PluginID     string          `json:"plugin_id"`
	Version      string          `json:"version"`
	WorkspaceRef string          `json:"workspace_ref"`
	Enabled      bool            `json:"enabled"`
	Settings     json.RawMessage `json:"settings,omitempty"`
	UpdatedAt    string          `json:"updated_at"`
}

type PluginRuntimeView struct {
	PluginID     string          `json:"plugin_id"`
	Version      string          `json:"version,omitempty"`
	WorkspaceRef string          `json:"workspace_ref,omitempty"`
	Status       string          `json:"status"`
	Details      json.RawMessage `json:"details,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
}

type PluginRegistryView struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Publisher       string   `json:"publisher"`
	Description     string   `json:"description,omitempty"`
	HostRequirement string   `json:"host_requirement"`
	Platforms       []string `json:"platforms,omitempty"`
	LatestVersion   string   `json:"latest_version,omitempty"`
}

func PrintPlugins(w io.Writer, outputFormat string, views []PluginView) error {
	switch outputFormat {
	case OutputText:
		tw := newTabWriter(w)
		if _, err := fmt.Fprintln(tw, "PLUGIN_ID\tVERSION\tNAME\tSOURCE\tACTIVE"); err != nil {
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\n",
				displayText(view.ID, "-"),
				displayText(view.Version, "-"),
				displayText(view.DisplayName, "-"),
				displayText(view.SourceKind, "-"),
				view.IsActive,
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	case OutputJSON:
		return writeJSON(w, views)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func PrintPlugin(w io.Writer, outputFormat string, view PluginView) error {
	switch outputFormat {
	case OutputText:
		_, err := fmt.Fprintf(w,
			"plugin_id: %s\nversion: %s\nname: %s\ndescription: %s\nsource: %s\nactive: %t\n",
			displayText(view.ID, "-"),
			displayText(view.Version, "-"),
			displayText(view.DisplayName, "-"),
			displayText(view.Description, "-"),
			displayText(view.SourceKind, "-"),
			view.IsActive,
		)
		return err
	case OutputJSON:
		return writeJSON(w, view)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func PrintPluginEnablement(w io.Writer, outputFormat string, view PluginEnablementView) error {
	switch outputFormat {
	case OutputText:
		_, err := fmt.Fprintf(w,
			"plugin_id: %s\nversion: %s\nworkspace_ref: %s\nenabled: %t\nsettings: %s\nupdated_at: %s\n",
			displayText(view.PluginID, "-"),
			displayText(view.Version, "-"),
			displayText(view.WorkspaceRef, "-"),
			view.Enabled,
			displayJSON(view.Settings),
			displayText(view.UpdatedAt, "-"),
		)
		return err
	case OutputJSON:
		return writeJSON(w, view)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func PrintPluginRuntime(w io.Writer, outputFormat string, view PluginRuntimeView) error {
	switch outputFormat {
	case OutputText:
		_, err := fmt.Fprintf(w,
			"plugin_id: %s\nversion: %s\nworkspace_ref: %s\nstatus: %s\ndetails: %s\nupdated_at: %s\n",
			displayText(view.PluginID, "-"),
			displayText(view.Version, "-"),
			displayText(view.WorkspaceRef, "-"),
			displayText(view.Status, "not_installed"),
			displayJSON(view.Details),
			displayText(view.UpdatedAt, "-"),
		)
		return err
	case OutputJSON:
		return writeJSON(w, view)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func PrintPluginRegistry(w io.Writer, outputFormat string, views []PluginRegistryView) error {
	switch outputFormat {
	case OutputText:
		tw := newTabWriter(w)
		if _, err := fmt.Fprintln(tw, "PLUGIN_ID\tNAME\tPUBLISHER\tVERSION\tHOST\tPLATFORMS"); err != nil {
			return err
		}
		for _, view := range views {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				displayText(view.ID, "-"),
				displayText(view.Name, "-"),
				displayText(view.Publisher, "-"),
				displayText(view.LatestVersion, "-"),
				displayText(view.HostRequirement, "-"),
				strings.Join(view.Platforms, ","),
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	case OutputJSON:
		return writeJSON(w, views)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func displayJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

package auth

import (
	"errors"
	"os"
)

// Report describes where credentials would come from and what is wrong with
// them, without exposing a token. It exists for diagnostics such as doctor.
type Report struct {
	// Source is "environment" for the COOLSHIP_URL/COOLSHIP_TOKEN pair or
	// "file" for a Coolify CLI configuration.
	Source        string      `json:"source"`
	Path          string      `json:"path,omitempty"`
	Exists        bool        `json:"exists"`
	Mode          os.FileMode `json:"-"`
	WorldReadable bool        `json:"world_readable"`
	Instances     []Instance  `json:"instances,omitempty"`
	Default       string      `json:"default,omitempty"`
	Err           error       `json:"-"`
}

// Inspect reports credential availability. It never fails: every problem is
// carried in the report so a diagnostic can show several at once.
func Inspect(options Options) Report {
	if value, supplied, err := explicitPair(options); supplied || err != nil {
		report := Report{Source: "environment", Exists: true, Err: err}
		if err == nil {
			report.Instances = []Instance{{Name: value.Name, URL: value.URL, Default: true}}
			report.Default = value.Name
		}
		return report
	}
	report := Report{Source: "file", Path: options.ConfigPath}
	if report.Path == "" {
		path, err := DefaultPath()
		if err != nil {
			report.Err = err
			return report
		}
		report.Path = path
	}
	info, err := os.Stat(report.Path)
	if errors.Is(err, os.ErrNotExist) {
		report.Err = err
		return report
	}
	if err != nil {
		report.Err = err
		return report
	}
	report.Exists = true
	report.Mode = info.Mode().Perm()
	report.WorldReadable = report.Mode&0o004 != 0
	instances, err := load(report.Path)
	if err != nil {
		report.Err = err
		return report
	}
	for _, instance := range instances {
		report.Instances = append(report.Instances, Instance{Name: instance.Name, URL: instance.FQDN, Default: instance.Default})
		if instance.Default {
			report.Default = instance.Name
		}
	}
	return report
}

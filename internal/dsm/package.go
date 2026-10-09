package dsm

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Package is one entry of SYNO.Core.Package list: a package installed on
// the NAS.
type Package struct {
	// ID is like "WebStation".
	ID string `json:"id"`
	// Name is the display name, like "Web Station".
	Name string `json:"name"`
	// Version is like "4.2.3-0522".
	Version    string `json:"version"`
	Additional struct {
		// Status is "running" or "stop".
		Status string `json:"status"`
		// InstallType is "system" for packages that come with DSM, and
		// empty for ones the user installed.
		InstallType string `json:"install_type"`
	} `json:"additional"`
}

// Packages lists the installed packages.
func (c *Client) Packages(ctx context.Context) ([]Package, error) {
	params := url.Values{"additional": {`["status","install_type"]`}}
	var out struct {
		Packages []Package `json:"packages"`
	}
	if err := c.Call(ctx, "SYNO.Core.Package", 1, "list", params, &out); err != nil {
		return nil, err
	}
	return out.Packages, nil
}

// StorePackage is one entry of SYNO.Core.Package.Server list: the latest
// version of a package in Synology's Package Center.
type StorePackage struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// IsSecurityVersion is true when this version is a security update.
	IsSecurityVersion bool `json:"is_security_version"`
}

// StorePackages asks Package Center, through the NAS, for the packages
// available to this model. Third-party packages are not in the list.
// Version 1 of the API answers in another shape without the packages.
func (c *Client) StorePackages(ctx context.Context) ([]StorePackage, error) {
	var out struct {
		Packages []StorePackage `json:"packages"`
	}
	if err := c.Call(ctx, "SYNO.Core.Package.Server", 2, "list", nil, &out); err != nil {
		return nil, err
	}
	return out.Packages, nil
}

// PackageUpdate is an installed package with the update available for it.
type PackageUpdate struct {
	Package
	// InStore is false for packages Package Center does not know, such as
	// third-party ones.
	InStore bool
	// Latest is the newer version in Package Center, or "" when the
	// installed one is the latest.
	Latest string
	// Security is true when Latest is a security update.
	Security bool
}

// Available reports whether a newer version is available.
func (u PackageUpdate) Available() bool { return u.Latest != "" }

// PackageUpdates matches the installed packages against Package Center and
// returns them sorted by ID, ignoring case.
func PackageUpdates(installed []Package, store []StorePackage) []PackageUpdate {
	latest := make(map[string]StorePackage, len(store))
	for _, s := range store {
		latest[s.ID] = s
	}
	out := make([]PackageUpdate, 0, len(installed))
	for _, p := range installed {
		u := PackageUpdate{Package: p}
		if s, ok := latest[p.ID]; ok {
			u.InStore = true
			if c, ok := CompareVersions(s.Version, p.Version); ok && c > 0 {
				u.Latest = s.Version
				u.Security = s.IsSecurityVersion
			}
		}
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b PackageUpdate) int {
		return cmp.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
	})
	return out
}

// CompareVersions compares package versions like "4.2.3-0522": the parts
// between "." and "-" are compared as numbers from the left, or as strings
// when neither is a number. It returns -1, 0 or +1, and false when the
// versions cannot be compared.
func CompareVersions(a, b string) (int, bool) {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' })
	}
	as, bs := split(a), split(b)
	if len(as) == 0 || len(bs) == 0 {
		return 0, false
	}
	for i := range min(len(as), len(bs)) {
		an, aerr := strconv.ParseUint(as[i], 10, 64)
		bn, berr := strconv.ParseUint(bs[i], 10, 64)
		var c int
		switch {
		case aerr == nil && berr == nil:
			c = cmp.Compare(an, bn)
		case aerr != nil && berr != nil:
			c = cmp.Compare(as[i], bs[i])
		default:
			return 0, false
		}
		if c != 0 {
			return c, true
		}
	}
	return cmp.Compare(len(as), len(bs)), true
}

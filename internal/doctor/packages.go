package doctor

import (
	"context"
	"fmt"

	"github.com/babarot/syno/internal/dsm"
)

// checkPackageUpdate warns only about security updates. Package Center
// almost always has a few updates, and warning about all of them would keep
// doctor at warn.
func checkPackageUpdate(ctx context.Context, env *Env) (Result, error) {
	installed, err := env.Source.Packages(ctx)
	if err != nil {
		return Result{}, err
	}
	store, err := env.Source.StorePackages(ctx)
	if err != nil {
		return Result{}, err
	}
	var (
		f       findings
		updates int
	)
	for _, u := range dsm.PackageUpdates(installed, store) {
		if !u.Available() {
			continue
		}
		updates++
		if u.Security {
			f.add(Warn, "%s %s is a security update (installed %s)", u.ID, u.Latest, u.Version)
		}
	}
	if updates == 0 {
		return f.result("packages are up to date"), nil
	}
	return f.result(fmt.Sprintf("%s available, none for security", plural(updates, "update"))), nil
}

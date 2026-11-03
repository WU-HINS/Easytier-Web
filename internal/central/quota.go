package central

import (
	"context"
	"fmt"
	"sort"

	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
)

// InstanceLister reports what each online machine of a tenant is running. It is
// implemented by session.Manager; central only needs this narrow view.
type InstanceLister interface {
	OnlineInstances(userID int64) map[string][]string
}

// QuotaReport summarises one enforcement pass.
type QuotaReport struct {
	// Machines is how many machines were over budget.
	Machines int
	// Trimmed is how many instances were asked to stop.
	Trimmed int
	// Failed counts machines whose instruction could not be delivered.
	Failed int
}

// EnforceQuota closes the gap that the push path alone leaves open: a device can
// start instances on its own and merely report them in the heartbeat. When the
// tenant is over its allowance the console instructs the device to retain a
// deterministic subset (sorted by instance id) via RetainNetworkInstance.
//
// A quota of 0 disables enforcement.
func (r *Reconciler) EnforceQuota(ctx context.Context, userID int64, lister InstanceLister, quota int) (QuotaReport, error) {
	report := QuotaReport{}
	if quota <= 0 || lister == nil {
		return report, nil
	}
	usage := lister.OnlineInstances(userID)
	type entry struct {
		machine  string
		instance string
	}
	var all []entry
	for machine, instances := range usage {
		for _, instance := range instances {
			all = append(all, entry{machine: machine, instance: instance})
		}
	}
	if len(all) <= quota {
		return report, nil
	}
	// Deterministic retention: sorted by (machine, instance) so repeated passes
	// keep the same instances and never flap.
	sort.Slice(all, func(i, j int) bool {
		if all[i].machine != all[j].machine {
			return all[i].machine < all[j].machine
		}
		return all[i].instance < all[j].instance
	})
	keep := map[string][]*common.UUID{}
	for index, item := range all {
		if index < quota {
			if uuid := protoUUID(item.instance); uuid != nil {
				keep[item.machine] = append(keep[item.machine], uuid)
			}
			continue
		}
		report.Trimmed++
	}
	machines := make([]string, 0, len(keep))
	for machine := range keep {
		machines = append(machines, machine)
	}
	sort.Strings(machines)
	for _, machine := range machines {
		report.Machines++
		request := &manage.RetainNetworkInstanceRequest{InstIds: keep[machine]}
		if _, err := r.Sessions.Call(ctx, userID, machine, WebClientKey, MethodRetainNetworkInstance, request); err != nil {
			report.Failed++
			r.Logf("quota: retain on %s failed: %v", machine, err)
			continue
		}
		r.Logf("quota: user %d trimmed to %d instance(s) on %s", userID, len(keep[machine]), machine)
	}
	return report, nil
}

// EnforceQuotaAll walks every connected tenant. quotaFn is evaluated per call so
// an admin change takes effect on the next pass.
func (r *Reconciler) EnforceQuotaAll(ctx context.Context, lister InstanceLister, quotaFn func(ctx context.Context) int) (QuotaReport, error) {
	total := QuotaReport{}
	for _, userID := range r.connectedTenants(lister) {
		report, err := r.EnforceQuota(ctx, userID, lister, quotaFn(ctx))
		if err != nil {
			return total, fmt.Errorf("central: enforce quota for %d: %w", userID, err)
		}
		total.Machines += report.Machines
		total.Trimmed += report.Trimmed
		total.Failed += report.Failed
	}
	return total, nil
}

// connectedTenants lists tenants that currently have a live session.
func (r *Reconciler) connectedTenants(lister InstanceLister) []int64 {
	type userLister interface {
		ConnectedUsers() []int64
	}
	if source, ok := lister.(userLister); ok {
		return source.ConnectedUsers()
	}
	return nil
}

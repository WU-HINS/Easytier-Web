package central

import (
	"context"
	"errors"
	"time"

	"github.com/easytier/easytier-console/internal/db/bal"
)

// WhitelistAllows reports whether machineID may join without an explicit member
// row. The whitelist is a network setting ("whitelist" inside advanced): an exact
// machine id, or "*" for everything. It is what makes a cluster join quickly: the
// device heartbeats, the reconciler creates the membership.
func WhitelistAllows(advanced *string, machineID string) bool {
	overrides, err := ParseOverrides(advanced)
	if err != nil {
		return false
	}
	for _, entry := range overrides.Whitelist {
		if entry == "*" || entry == machineID {
			return true
		}
	}
	return false
}

// enrollWhitelisted adds a member row for every device the whitelist allows and
// that has none yet. Removing a member whose device is still whitelisted re-adds
// it on the next pass: that is the documented cost of an automatic join, so a
// permanent removal means taking the device out of the whitelist as well.
func (r *Reconciler) enrollWhitelisted(ctx context.Context, userID int64, networks []bal.Network, devices map[string]bal.Device) (int, error) {
	enrolled := 0
	now := time.Now().UTC()
	for _, network := range networks {
		if network.Advanced == nil || *network.Advanced == "" {
			continue
		}
		members, err := r.Store.ListMembers(ctx, userID, network.ID)
		if err != nil {
			return enrolled, err
		}
		present := make(map[string]bool, len(members))
		for _, member := range members {
			present[member.DeviceID] = true
		}
		for machineID := range devices {
			if present[machineID] || !WhitelistAllows(network.Advanced, machineID) {
				continue
			}
			member := bal.NetworkMember{
				UserID: userID, NetworkID: network.ID, DeviceID: machineID,
				ID:         network.ID + "/" + machineID,
				CreateTime: now, UpdateTime: now,
			}
			if err := r.Store.AddMember(ctx, member); err != nil {
				if errors.Is(err, bal.ErrMemberExists) {
					continue
				}
				return enrolled, err
			}
			enrolled++
		}
	}
	return enrolled, nil
}

package central_test

import (
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/central"
	"github.com/easytier/easytier-console/internal/db/bal"
	"github.com/easytier/easytier-console/internal/pb/api/manage"
)

func TestStandaloneNetworkCarriesItsPeerURLs(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	cidr := "10.170.0.0/29"
	network := bal.Network{
		ID: "net-peers", NetworkName: "peers", NetworkSecret: "s",
		NetworkingMethod: "Standalone", VirtualCIDR: &cidr,
		PeerURLs:   []string{"tcp://v4v6.usvps.server.ddns.hinswu.top:21010", "udp://127.0.0.1:11010"},
		CreateTime: now, UpdateTime: now,
	}
	member := bal.NetworkMember{ID: "m1", NetworkID: "net-peers", DeviceID: "dev-1", CreateTime: now, UpdateTime: now}
	devices := map[string]bal.Device{"dev-1": {MachineID: "dev-1", Hostname: "a"}}
	plan, err := central.Compile(network, []bal.NetworkMember{member}, devices, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Standalone means "no peers" upstream: a standalone network that lists peers
	// has to be pushed as Manual or the client drops them on the floor.
	if method := plan.Members[0].Config.GetNetworkingMethod(); method != manage.NetworkingMethod_Manual {
		t.Fatalf("networking method = %v, want Manual when peers are listed", method)
	}
	got := plan.Members[0].Config.GetPeerUrls()
	if len(got) != 2 {
		t.Fatalf("peer urls = %v, want both entries", got)
	}
}

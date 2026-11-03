// Command smokedevice is a minimal EasyTier device used by the deployment
// smoke test. It registers the control-plane channel, heartbeats, and waits
// until the console pushes a network configuration.
//
// Exit code 0 means "a config arrived"; anything else means the chain
// (intent -> compile -> reconcile -> push) is broken somewhere.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/easytier/easytier-console/internal/central"
	manage "github.com/easytier/easytier-console/internal/pb/api/manage"
	"github.com/easytier/easytier-console/internal/pb/common"
	"github.com/easytier/easytier-console/internal/pb/web"
	"github.com/easytier/easytier-console/internal/rpc"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/wire"
)

func main() {
	transport := flag.String("transport", "tcp", "tcp | ws | udp")
	endpoint := flag.String("endpoint", "127.0.0.1:22020", "control-plane address (host:port)")
	token := flag.String("token", "", "device token (console username)")
	machine := flag.String("machine", "", "machine id (uuid)")
	timeout := flag.Duration("timeout", 40*time.Second, "how long to wait for a pushed config")
	expectNetwork := flag.String("expect-network", "", "require this network name in the pushed config")
	expectIPv4 := flag.String("expect-ipv4", "", "require this virtual IPv4 in the pushed config")
	expectCount := flag.Int("expect-count", 1, "number of distinct configurations to wait for")
	flag.Parse()

	if *token == "" || *machine == "" {
		fmt.Fprintln(os.Stderr, "smokedevice: --token and --machine are required")
		os.Exit(2)
	}
	expectations := expectations{
		network: *expectNetwork,
		ipv4:    *expectIPv4,
		count:   *expectCount,
	}
	if err := run(*transport, *endpoint, *token, *machine, *timeout, expectations); err != nil {
		fmt.Fprintln(os.Stderr, "smokedevice:", err)
		os.Exit(1)
	}
	fmt.Println("DEVICE_OK")
}

type expectations struct {
	network string
	ipv4    string
	count   int
}

func run(transport, endpoint, token, machine string, timeout time.Duration, want expectations) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var (
		tunnel wire.Tunnel
		err    error
	)
	switch transport {
	case "tcp":
		tunnel, err = wire.DialTCP(ctx, endpoint)
	case "udp":
		tunnel, err = wire.DialUDP(ctx, endpoint)
	case "ws":
		url := endpoint
		if !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
			url = "ws://" + url
		}
		if !strings.Contains(url[len("ws://"):], "/") {
			url += "/"
		}
		tunnel, err = wire.DialWS(ctx, url, nil)
	default:
		return fmt.Errorf("unknown transport %q", transport)
	}
	if err != nil {
		return fmt.Errorf("dial %s (%s): %w", endpoint, transport, err)
	}
	defer tunnel.Close()

	machineID, err := parseUUID(machine)
	if err != nil {
		return err
	}

	received := make(chan *manage.NetworkConfig, 1)
	registry := rpc.NewRegistry()
	if err := registry.Register(central.WebClientKey, central.MethodRunNetworkInstance,
		func(ctx context.Context, body []byte) ([]byte, error) {
			request := &manage.RunNetworkInstanceRequest{}
			if err := proto.Unmarshal(body, request); err != nil {
				return nil, err
			}
			select {
			case received <- request.GetConfig():
			default:
			}
			return proto.Marshal(&manage.RunNetworkInstanceResponse{})
		}); err != nil {
		return err
	}

	mux := rpc.NewMux(tunnel, registry, rpc.Options{})
	go func() { _ = mux.Run(ctx) }()
	defer mux.Close()

	heartbeat := &web.HeartbeatRequest{
		UserToken:       token,
		MachineId:       machineID,
		Hostname:        "smoke-device",
		EasytierVersion: "smoke",
	}
	for {
		if _, err := mux.Call(ctx, session.WebServerKey, 1, heartbeat); err == nil {
			break
		} else if ctx.Err() != nil {
			return fmt.Errorf("heartbeat never accepted: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	seen := map[string]bool{}
	for {
		select {
		case config := <-received:
			line := "network=" + config.GetNetworkName() +
				" virtual_ipv4=" + config.GetVirtualIpv4() +
				" hostname=" + config.GetHostname() +
				" mtu=" + fmt.Sprintf("%d", config.GetMtu())
			if want.network != "" && config.GetNetworkName() != want.network {
				fmt.Println("CONFIG_MISMATCH", line)
				return fmt.Errorf("expected network %q, got %q", want.network, config.GetNetworkName())
			}
			if want.ipv4 != "" && config.GetVirtualIpv4() != want.ipv4 {
				fmt.Println("CONFIG_MISMATCH", line)
				return fmt.Errorf("expected virtual IPv4 %q, got %q", want.ipv4, config.GetVirtualIpv4())
			}
			if seen[line] {
				continue
			}
			seen[line] = true
			fmt.Println("CONFIG_RECEIVED", line)
			if len(seen) >= want.count {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("only %d of %d configuration(s) arrived within %s", len(seen), want.count, timeout)
		}
	}
}

func parseUUID(value string) (*common.UUID, error) {
	cleaned := strings.ReplaceAll(value, "-", "")
	if len(cleaned) != 32 {
		return nil, fmt.Errorf("machine id %q is not a uuid", value)
	}
	var raw [16]byte
	for index := 0; index < 16; index++ {
		var high, low byte
		if _, err := fmt.Sscanf(cleaned[index*2:index*2+1], "%x", &high); err != nil {
			return nil, fmt.Errorf("machine id %q is not a uuid", value)
		}
		if _, err := fmt.Sscanf(cleaned[index*2+1:index*2+2], "%x", &low); err != nil {
			return nil, fmt.Errorf("machine id %q is not a uuid", value)
		}
		raw[index] = high<<4 | low
	}
	return &common.UUID{
		Part1: uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]),
		Part2: uint32(raw[4])<<24 | uint32(raw[5])<<16 | uint32(raw[6])<<8 | uint32(raw[7]),
		Part3: uint32(raw[8])<<24 | uint32(raw[9])<<16 | uint32(raw[10])<<8 | uint32(raw[11]),
		Part4: uint32(raw[12])<<24 | uint32(raw[13])<<16 | uint32(raw[14])<<8 | uint32(raw[15]),
	}, nil
}

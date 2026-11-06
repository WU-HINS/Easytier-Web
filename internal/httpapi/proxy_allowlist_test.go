package httpapi

import "testing"

// The allowlist is the only thing standing between the proxy and every call in the
// method index, so the interesting cases are the ones that must be refused.
func TestProxyAllowlistRefusesWrites(t *testing.T) {
	refused := []struct{ service, method string }{
		{"api.manage.WebClientService", "RunNetworkInstance"},
		{"api.manage.WebClientService", "DeleteNetworkInstance"},
		{"api.manage.WebClientService", "RetainNetworkInstance"},
		{"api.instance.CredentialManageRpc", "GenerateCredential"},
		{"api.instance.CredentialManageRpc", "RevokeCredential"},
		{"api.instance.CredentialManageRpc", "UpsertCredential"},
		{"api.logger.LoggerRpc", "SetLoggerConfig"},
		{"magic_dns.MagicDnsServerRpc", "UpdateDnsRecord"},
		{"peer_rpc.OspfRouteRpc", "SyncRouteInfo"},
		{"api.instance.AclManageRpc", "DropAllRules"},
		{"not.a.Service", "Anything"},
		{"", ""},
	}
	for _, entry := range refused {
		if proxyMethodAllowed(entry.service, entry.method) {
			t.Fatalf("%s/%s must not be reachable through the proxy", entry.service, entry.method)
		}
	}
}

func TestProxyAllowlistPermitsReads(t *testing.T) {
	allowed := []struct{ service, method string }{
		{"api.instance.PeerManageRpc", "ListPeer"},
		{"api.instance.PeerManageRpc", "ShowNodeInfo"},
		{"api.instance.AclManageRpc", "GetAclStats"},
		{"api.instance.StatsRpc", "GetPrometheusStats"},
		{"api.instance.CredentialManageRpc", "ListCredentials"},
		{"api.logger.LoggerRpc", "GetLoggerConfig"},
	}
	for _, entry := range allowed {
		if !proxyMethodAllowed(entry.service, entry.method) {
			t.Fatalf("%s/%s should be reachable through the proxy", entry.service, entry.method)
		}
	}
}

// Every allowlisted entry has to exist in the method index, otherwise the console
// would advertise a call that can never be forwarded.
func TestProxyAllowlistMatchesTheMethodIndex(t *testing.T) {
	for service, methods := range proxyReadOnlyMethods {
		for method := range methods {
			if _, ok := lookupMethod(service, method); !ok {
				t.Fatalf("allowlisted %s/%s is not in the method index", service, method)
			}
		}
	}
}

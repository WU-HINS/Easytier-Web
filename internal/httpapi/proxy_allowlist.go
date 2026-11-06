package httpapi

// The generic proxy-rpc endpoint forwards any method the method index knows, and
// that index includes calls that change a device: starting or deleting a network
// instance, issuing credentials, rewriting DNS records. A tenant can only reach
// their own devices through it, but "we could not find a hole" is not the same as
// "there is no hole": the console has purpose-built endpoints for the writes it
// actually needs, so the proxy only has to serve reads.
//
// Everything not listed here is refused. Adding a method is a deliberate act.
var proxyReadOnlyMethods = map[string]map[string]bool{
	"api.instance.PeerManageRpc": {
		"ListPeer":                 true,
		"ListPublicIpv6Info":       true,
		"ListRoute":                true,
		"DumpRoute":                true,
		"ListForeignNetwork":       true,
		"ListGlobalForeignNetwork": true,
		"ShowNodeInfo":             true,
		"GetForeignNetworkSummary": true,
	},
	"api.instance.ConnectorManageRpc": {
		"ListConnector": true,
	},
	"api.instance.MappedListenerManageRpc": {
		"ListMappedListener": true,
	},
	"api.instance.VpnPortalRpc": {
		"GetVpnPortalInfo": true,
	},
	"api.instance.TcpProxyRpc": {
		"ListTcpProxyEntry": true,
	},
	"api.instance.AclManageRpc": {
		"GetAclStats":  true,
		"GetWhitelist": true,
	},
	"api.instance.PortForwardManageRpc": {
		"ListPortForward": true,
	},
	"api.instance.StatsRpc": {
		"GetStats":           true,
		"GetPrometheusStats": true,
	},
	"api.instance.CredentialManageRpc": {
		"ListCredentials": true,
	},
	"api.logger.LoggerRpc": {
		"GetLoggerConfig": true,
	},
}

// proxyMethodAllowed reports whether the generic proxy may forward this call.
func proxyMethodAllowed(service, method string) bool {
	methods, ok := proxyReadOnlyMethods[service]
	if !ok {
		return false
	}
	return methods[method]
}

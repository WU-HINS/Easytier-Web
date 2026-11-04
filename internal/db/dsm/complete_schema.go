package dsm

// All is the single source of truth for the control-plane data model. DDL for
// every dialect, the introspection diff and the alignment plan are derived from
// this declaration, so the two dialects can never drift apart.
func All() []Table {
	return []Table{
		{
			Name: "schema_state",
			Columns: []Column{
				{Name: "id", Kind: Int, NotNull: true, PrimaryKey: true},
				{Name: "fingerprint", Kind: Text, NotNull: true},
				{Name: "applied_at", Kind: Timestamp, NotNull: true},
			},
		},
		{
			Name: "users",
			Columns: []Column{
				{Name: "id", Kind: Serial, NotNull: true, PrimaryKey: true},
				{Name: "username", Kind: Text, NotNull: true},
				{Name: "password", Kind: Text, NotNull: true},
				{Name: "email", Kind: Text},
				{Name: "oidc_subject", Kind: Text},
				{Name: "oidc_issuer", Kind: Text},
				{Name: "display_name", Kind: Text},
				{Name: "role", Kind: Text, NotNull: true, Default: "'user'"},
				{Name: "disabled", Kind: Bool, NotNull: true, Default: "false"},
				{Name: "created_at", Kind: Timestamp, NotNull: true},
			},
			Indexes: []Index{
				{Name: "uq_users_username", Columns: []string{"username"}, Unique: true},
				// Not partial: NULL subjects stay distinct, so a plain unique
				// index also lets the upsert use ON CONFLICT.
				{Name: "uq_users_oidc_identity", Columns: []string{"oidc_issuer", "oidc_subject"}, Unique: true},
				// The single administrator invariant is enforced by the database, not by a
				// read-then-write check: two concurrent setups must not both insert.
				{Name: "uq_users_single_admin", Columns: []string{"role"}, Unique: true, Where: "role = 'admin'"},
			},
		},
		{
			Name: "console_instances",
			Columns: []Column{
				{Name: "id", Kind: Text, NotNull: true, PrimaryKey: true},
				{Name: "version", Kind: Text, NotNull: true, Default: "''"},
				{Name: "started_at", Kind: Timestamp, NotNull: true},
				{Name: "last_seen", Kind: Timestamp, NotNull: true},
			},
			Indexes: []Index{
				{Name: "idx_console_instances_seen", Columns: []string{"last_seen"}},
			},
		},
		{
			Name: "settings",
			Columns: []Column{
				{Name: "key", Kind: Text, NotNull: true, PrimaryKey: true},
				{Name: "value", Kind: Text, NotNull: true},
				{Name: "updated_at", Kind: Timestamp, NotNull: true},
			},
		},
		{
			Name: "sessions",
			Columns: []Column{
				{Name: "sid", Kind: Text, NotNull: true, PrimaryKey: true},
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "data", Kind: Text, NotNull: true, Default: "''"},
				{Name: "expiry", Kind: Timestamp, NotNull: true},
				{Name: "created_at", Kind: Timestamp, NotNull: true},
			},
			Indexes: []Index{
				{Name: "idx_sessions_expiry", Columns: []string{"expiry"}},
				{Name: "idx_sessions_user", Columns: []string{"user_id"}},
			},
			References: []Reference{
				{Columns: []string{"user_id"}, Table: "users", RefCols: []string{"id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "devices",
			Columns: []Column{
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "machine_id", Kind: Text, NotNull: true},
				{Name: "hostname", Kind: Text, NotNull: true, Default: "''"},
				{Name: "alias", Kind: Text, NotNull: true, Default: "''"},
				{Name: "easytier_version", Kind: Text, NotNull: true, Default: "''"},
				{Name: "device_os", Kind: Text, NotNull: true, Default: "'{}'"},
				{Name: "client_url", Kind: Text, NotNull: true, Default: "''"},
				{Name: "first_seen_time", Kind: Timestamp, NotNull: true},
				{Name: "last_seen_time", Kind: Timestamp, NotNull: true},
			},
			PrimaryKey: []string{"user_id", "machine_id"},
			Indexes: []Index{
				{Name: "idx_devices_user_last_seen", Columns: []string{"user_id", "last_seen_time"}},
			},
			References: []Reference{
				{Columns: []string{"user_id"}, Table: "users", RefCols: []string{"id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "blocked_devices",
			Columns: []Column{
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "machine_id", Kind: Text, NotNull: true},
				{Name: "hostname", Kind: Text, NotNull: true, Default: "''"},
				{Name: "blocked_time", Kind: Timestamp, NotNull: true},
				{Name: "attempt_count", Kind: Int, NotNull: true, Default: "0"},
				{Name: "last_attempt_time", Kind: Timestamp},
			},
			PrimaryKey: []string{"user_id", "machine_id"},
			Checks:     []string{"attempt_count >= 0"},
			References: []Reference{
				{Columns: []string{"user_id"}, Table: "users", RefCols: []string{"id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "managed_config_revisions",
			Columns: []Column{
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "machine_id", Kind: Text, NotNull: true},
				{Name: "config_revision", Kind: Text, NotNull: true},
				{Name: "update_time", Kind: Timestamp, NotNull: true},
			},
			PrimaryKey: []string{"user_id", "machine_id"},
			References: []Reference{
				{Columns: []string{"user_id"}, Table: "users", RefCols: []string{"id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "user_running_network_configs",
			Columns: []Column{
				{Name: "id", Kind: Serial, NotNull: true, PrimaryKey: true},
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "device_id", Kind: Text, NotNull: true},
				{Name: "network_instance_id", Kind: Text, NotNull: true},
				{Name: "network_config", Kind: Text, NotNull: true},
				{Name: "disabled", Kind: Bool, NotNull: true, Default: "false"},
				{Name: "source", Kind: Text, NotNull: true, Default: "'web'"},
				{Name: "create_time", Kind: Timestamp, NotNull: true},
				{Name: "update_time", Kind: Timestamp, NotNull: true},
			},
			Indexes: []Index{
				{Name: "uq_running_configs_instance", Columns: []string{"network_instance_id"}, Unique: true},
				{Name: "idx_running_configs_user", Columns: []string{"user_id", "device_id"}},
			},
			References: []Reference{
				{Columns: []string{"user_id"}, Table: "users", RefCols: []string{"id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "networks",
			Columns: []Column{
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "id", Kind: Text, NotNull: true},
				{Name: "display_name", Kind: Text, NotNull: true},
				{Name: "network_name", Kind: Text, NotNull: true},
				{Name: "network_secret", Kind: Text, NotNull: true},
				{Name: "networking_method", Kind: Text, NotNull: true},
				{Name: "public_server_url", Kind: Text},
				{Name: "peer_urls", Kind: Text, NotNull: true, Default: "'[]'"},
				{Name: "virtual_cidr", Kind: Text},
				{Name: "secure_mode", Kind: Bool, NotNull: true, Default: "false"},
				{Name: "acl_policy", Kind: Text},
				{Name: "advanced", Kind: Text, NotNull: true, Default: "''"},
				{Name: "create_time", Kind: Timestamp, NotNull: true},
				{Name: "update_time", Kind: Timestamp, NotNull: true},
			},
			PrimaryKey: []string{"user_id", "id"},
			Checks: []string{
				"networking_method IN ('PublicServer','Manual','Standalone','Gateway')",
			},
			Indexes: []Index{
				{Name: "uq_networks_tenant_name", Columns: []string{"user_id", "network_name"}, Unique: true},
				{Name: "uq_networks_gateway_name", Columns: []string{"network_name"}, Unique: true, Where: "networking_method = 'Gateway'"},
			},
			References: []Reference{
				{Columns: []string{"user_id"}, Table: "users", RefCols: []string{"id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "network_credentials",
			Columns: []Column{
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "network_id", Kind: Text, NotNull: true},
				{Name: "credential_id", Kind: Text, NotNull: true},
				{Name: "credential_secret", Kind: Text, NotNull: true},
				{Name: "expiry_unix", Kind: BigInt, NotNull: true},
				{Name: "acl_groups", Kind: Text, NotNull: true, Default: "'[]'"},
				{Name: "allow_relay", Kind: Bool, NotNull: true, Default: "false"},
				{Name: "allowed_proxy_cidrs", Kind: Text, NotNull: true, Default: "'[]'"},
				{Name: "reusable", Kind: Bool, NotNull: true, Default: "false"},
				{Name: "create_time", Kind: Timestamp, NotNull: true},
				{Name: "update_time", Kind: Timestamp, NotNull: true},
			},
			PrimaryKey: []string{"user_id", "network_id", "credential_id"},
			References: []Reference{
				{Columns: []string{"user_id", "network_id"}, Table: "networks", RefCols: []string{"user_id", "id"}, OnDelete: "CASCADE"},
			},
		},
		{
			Name: "network_members",
			Columns: []Column{
				{Name: "user_id", Kind: BigInt, NotNull: true},
				{Name: "network_id", Kind: Text, NotNull: true},
				{Name: "id", Kind: Text, NotNull: true},
				{Name: "device_id", Kind: Text, NotNull: true},
				{Name: "hostname_override", Kind: Text},
				{Name: "virtual_ipv4", Kind: Text},
				{Name: "allocated_ipv4", Kind: Text},
				{Name: "override_config", Kind: Text},
				{Name: "credential_id", Kind: Text},
				{Name: "acl_group_secret", Kind: Text},
				{Name: "create_time", Kind: Timestamp, NotNull: true},
				{Name: "update_time", Kind: Timestamp, NotNull: true},
			},
			PrimaryKey: []string{"user_id", "network_id", "id"},
			Checks: []string{
				"credential_id IS NULL OR acl_group_secret IS NULL",
			},
			Indexes: []Index{
				{Name: "uq_network_members_device", Columns: []string{"user_id", "network_id", "device_id"}, Unique: true},
				{Name: "uq_network_members_credential", Columns: []string{"user_id", "network_id", "credential_id"}, Unique: true, Where: "credential_id IS NOT NULL"},
			},
			References: []Reference{
				{Columns: []string{"user_id", "network_id"}, Table: "networks", RefCols: []string{"user_id", "id"}, OnDelete: "CASCADE"},
				{Columns: []string{"user_id", "device_id"}, Table: "devices", RefCols: []string{"user_id", "machine_id"}, OnDelete: "CASCADE"},
			},
		},
	}
}

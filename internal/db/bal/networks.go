package bal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dal/query"
)

const networkColumns = "user_id, id, display_name, network_name, network_secret, networking_method, public_server_url, peer_urls, virtual_cidr, secure_mode, acl_policy, create_time, update_time"

const memberColumns = "user_id, network_id, id, device_id, hostname_override, virtual_ipv4, allocated_ipv4, override_config, credential_id, acl_group_secret, create_time, update_time"

func encodeStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeStrings(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

// CreateNetwork inserts a network intent.
func (s *Store) CreateNetwork(ctx context.Context, network Network) error {
	statement, args := query.Into("networks").
		Columns("user_id", "id", "display_name", "network_name", "network_secret", "networking_method",
			"public_server_url", "peer_urls", "virtual_cidr", "secure_mode", "acl_policy", "create_time", "update_time").
		Values(network.UserID, network.ID, network.DisplayName, network.NetworkName, network.NetworkSecret,
			network.NetworkingMethod, network.PublicServerURL, encodeStrings(network.PeerURLs),
			network.VirtualCIDR, dal.BoolArg(s.Dialect(), network.SecureMode), network.ACLPolicy,
			dal.TimeArg(s.Dialect(), network.CreateTime), dal.TimeArg(s.Dialect(), network.UpdateTime)).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		if isNetworkNameViolation(err) {
			return ErrNetworkExists
		}
		return fmt.Errorf("bal: create network: %w", err)
	}
	return nil
}

func (s *Store) scanNetwork(row dal.Row) (Network, error) {
	var (
		network     Network
		publicURL   sql.NullString
		peers       string
		virtualCIDR sql.NullString
		secureMode  any
		aclPolicy   sql.NullString
		created     any
		updated     any
	)
	err := row.Scan(&network.UserID, &network.ID, &network.DisplayName, &network.NetworkName,
		&network.NetworkSecret, &network.NetworkingMethod, &publicURL, &peers, &virtualCIDR,
		&secureMode, &aclPolicy, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Network{}, ErrNotFound
	}
	if err != nil {
		return Network{}, fmt.Errorf("bal: scan network: %w", err)
	}
	if publicURL.Valid {
		value := publicURL.String
		network.PublicServerURL = &value
	}
	if virtualCIDR.Valid {
		value := virtualCIDR.String
		network.VirtualCIDR = &value
	}
	if aclPolicy.Valid {
		value := aclPolicy.String
		network.ACLPolicy = &value
	}
	network.PeerURLs = decodeStrings(peers)
	network.SecureMode = dal.ParseBool(secureMode)
	if network.CreateTime, err = dal.ParseTime(created); err != nil {
		return Network{}, err
	}
	if network.UpdateTime, err = dal.ParseTime(updated); err != nil {
		return Network{}, err
	}
	return network, nil
}

// GetNetwork loads one intent.
func (s *Store) GetNetwork(ctx context.Context, userID int64, id string) (Network, error) {
	statement, args := query.From("networks").Columns(splitColumns(networkColumns)...).
		Where("user_id", "=", userID).Where("id", "=", id).Build(s.Dialect())
	return s.scanNetwork(s.conn.QueryRow(ctx, statement, args...))
}

// ListNetworks lists every intent owned by a tenant.
func (s *Store) ListNetworks(ctx context.Context, userID int64) ([]Network, error) {
	statement, args := query.From("networks").Columns(splitColumns(networkColumns)...).
		Where("user_id", "=", userID).OrderBy("create_time", false).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list networks: %w", err)
	}
	defer rows.Close()
	var out []Network
	for rows.Next() {
		network, err := s.scanNetwork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, network)
	}
	return out, rows.Err()
}

// UpdateNetwork writes the mutable part of an intent.
func (s *Store) UpdateNetwork(ctx context.Context, network Network) error {
	statement, args := query.Table("networks").
		Set("display_name", network.DisplayName).
		Set("network_name", network.NetworkName).
		Set("network_secret", network.NetworkSecret).
		Set("networking_method", network.NetworkingMethod).
		Set("public_server_url", network.PublicServerURL).
		Set("peer_urls", encodeStrings(network.PeerURLs)).
		Set("virtual_cidr", network.VirtualCIDR).
		Set("secure_mode", dal.BoolArg(s.Dialect(), network.SecureMode)).
		Set("acl_policy", network.ACLPolicy).
		Set("update_time", dal.TimeArg(s.Dialect(), network.UpdateTime)).
		Where("user_id", "=", network.UserID).Where("id", "=", network.ID).
		Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("bal: update network: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteNetwork removes an intent; members and credentials cascade.
func (s *Store) DeleteNetwork(ctx context.Context, userID int64, id string) error {
	statement, args := query.DeleteFrom("networks").
		Where("user_id", "=", userID).Where("id", "=", id).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("bal: delete network: %w", err)
	}
	// A foreign or unknown id deletes nothing; saying 204 would tell the caller
	// it succeeded (found by the review team in r135).
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrNetworkExists means the tenant already has a network with that name.
var ErrNetworkExists = errors.New("bal: a network with this name already exists")

// isNetworkNameViolation narrows the generic uniqueness check to the network
// name index, so a future constraint cannot be mistaken for this one.
func isNetworkNameViolation(err error) bool {
	if !isUniqueViolation(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "networks.") || strings.Contains(message, "uq_networks_")
}

// ErrMemberExists is returned when a device is already a member of the network.
var ErrMemberExists = errors.New("bal: device is already a member")

// ErrDeviceNotFound is returned when a membership references a device the
// tenant does not own.
var ErrDeviceNotFound = errors.New("bal: device not found")

// isUniqueViolation reports a driver specific uniqueness failure. It uses the
// same matching as classifyInsertError but returns a boolean so callers can map it
// to their own sentinel.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "sqlstate 23505") ||
		strings.Contains(message, "duplicate key value")
}

// classifyInsertError turns driver specific constraint failures into the two
// outcomes callers can act on. Matching on the message is not pretty, but it is
// portable across SQLite and PostgreSQL without leaking driver details upward.
func classifyInsertError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "unique constraint failed"),
		strings.Contains(message, "sqlstate 23505"),
		strings.Contains(message, "duplicate key value"):
		return ErrMemberExists
	case strings.Contains(message, "foreign key constraint failed"),
		strings.Contains(message, "sqlstate 23503"),
		strings.Contains(message, "violates foreign key"):
		return ErrDeviceNotFound
	default:
		return err
	}
}

// ErrQuotaExceeded is returned when a membership would push the tenant past
// its allowance.
var ErrQuotaExceeded = errors.New("bal: instance quota exceeded")

// AddMembersBounded is AddMembers with the quota enforced inside the same
// transaction. A count-then-insert in the handler is not enough: on PostgreSQL
// two concurrent requests both read the same count and both insert. Taking the
// tenant row lock first serialises writers, so the count is read after the
// previous writer committed.
func (s *Store) AddMembersBounded(ctx context.Context, userID int64, members []NetworkMember, quota int) error {
	if len(members) == 0 {
		return nil
	}
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bal: begin members: %w", err)
	}
	// Row lock on the tenant: a no-op update is the portable way to take it.
	lock, lockArgs := query.Table("users").
		Set("id", userID).
		Where("id", "=", userID).
		Build(s.Dialect())
	if _, err := tx.Exec(ctx, lock, lockArgs...); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("bal: lock tenant: %w", err)
	}
	if quota > 0 {
		countStatement := "SELECT COUNT(*) FROM " + query.Quote("network_members") +
			" WHERE " + query.Quote("user_id") + " = " + query.Placeholder(s.Dialect(), 1)
		countArgs := []any{userID}
		var current int
		if err := tx.QueryRow(ctx, countStatement, countArgs...).Scan(&current); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("bal: count members: %w", err)
		}
		if current+len(members) > quota {
			_ = tx.Rollback()
			return ErrQuotaExceeded
		}
	}
	for _, member := range members {
		statement, args := query.Into("network_members").
			Columns("user_id", "network_id", "id", "device_id", "hostname_override", "virtual_ipv4", "allocated_ipv4", "override_config", "credential_id", "acl_group_secret", "create_time", "update_time").
			Values(member.UserID, member.NetworkID, member.ID, member.DeviceID, member.HostnameOverride,
				member.VirtualIPv4, member.AllocatedIPv4, member.OverrideConfig, member.CredentialID,
				member.ACLGroupSecret, dal.TimeArg(s.Dialect(), member.CreateTime), dal.TimeArg(s.Dialect(), member.UpdateTime)).
			Build(s.Dialect())
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			_ = tx.Rollback()
			if classified := classifyInsertError(err); classified != err {
				return classified
			}
			return fmt.Errorf("bal: add member failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bal: commit members: %w", err)
	}
	return nil
}

// AddMembers binds several devices in one transaction: either every membership
// is created or none is, so a bad device cannot leave a half applied batch.
func (s *Store) AddMembers(ctx context.Context, members []NetworkMember) error {
	if len(members) == 0 {
		return nil
	}
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bal: begin members: %w", err)
	}
	for _, member := range members {
		statement, args := query.Into("network_members").
			Columns("user_id", "network_id", "id", "device_id", "hostname_override", "virtual_ipv4", "allocated_ipv4", "override_config", "credential_id", "acl_group_secret", "create_time", "update_time").
			Values(member.UserID, member.NetworkID, member.ID, member.DeviceID, member.HostnameOverride,
				member.VirtualIPv4, member.AllocatedIPv4, member.OverrideConfig, member.CredentialID,
				member.ACLGroupSecret, dal.TimeArg(s.Dialect(), member.CreateTime), dal.TimeArg(s.Dialect(), member.UpdateTime)).
			Build(s.Dialect())
		if _, err := tx.Exec(ctx, statement, args...); err != nil {
			_ = tx.Rollback()
			if classified := classifyInsertError(err); classified != err {
				return classified
			}
			// Never surface driver text: it names tables and columns.
			return fmt.Errorf("bal: add member failed")
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bal: commit members: %w", err)
	}
	return nil
}

func (s *Store) scanMember(row dal.Row) (NetworkMember, error) {
	var (
		member         NetworkMember
		hostname       sql.NullString
		virtualIPv4    sql.NullString
		allocatedIPv4  sql.NullString
		overrideConfig sql.NullString
		credentialID   sql.NullString
		aclGroupSecret sql.NullString
		created        any
		updated        any
	)
	err := row.Scan(&member.UserID, &member.NetworkID, &member.ID, &member.DeviceID, &hostname,
		&virtualIPv4, &allocatedIPv4, &overrideConfig, &credentialID, &aclGroupSecret, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return NetworkMember{}, ErrNotFound
	}
	if err != nil {
		return NetworkMember{}, fmt.Errorf("bal: scan member: %w", err)
	}
	member.HostnameOverride = nullPtr(hostname)
	member.VirtualIPv4 = nullPtr(virtualIPv4)
	member.AllocatedIPv4 = nullPtr(allocatedIPv4)
	member.OverrideConfig = nullPtr(overrideConfig)
	member.CredentialID = nullPtr(credentialID)
	member.ACLGroupSecret = nullPtr(aclGroupSecret)
	if member.CreateTime, err = dal.ParseTime(created); err != nil {
		return NetworkMember{}, err
	}
	if member.UpdateTime, err = dal.ParseTime(updated); err != nil {
		return NetworkMember{}, err
	}
	return member, nil
}

func nullPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

// ListMembers returns the members of a network.
func (s *Store) ListMembers(ctx context.Context, userID int64, networkID string) ([]NetworkMember, error) {
	statement, args := query.From("network_members").Columns(splitColumns(memberColumns)...).
		Where("user_id", "=", userID).Where("network_id", "=", networkID).
		OrderBy("create_time", false).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list members: %w", err)
	}
	defer rows.Close()
	var out []NetworkMember
	for rows.Next() {
		member, err := s.scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, member)
	}
	return out, rows.Err()
}

// GetMemberByDevice finds the membership of one device in one network.
func (s *Store) GetMemberByDevice(ctx context.Context, userID int64, networkID, deviceID string) (NetworkMember, error) {
	statement, args := query.From("network_members").Columns(splitColumns(memberColumns)...).
		Where("user_id", "=", userID).Where("network_id", "=", networkID).
		Where("device_id", "=", deviceID).Build(s.Dialect())
	return s.scanMember(s.conn.QueryRow(ctx, statement, args...))
}

// MemberPatch names the columns a caller wants to change. A nil field means
// "leave it alone": writing the whole row back would silently undo a concurrent
// edit to another column (review team, r140).
type MemberPatch struct {
	HostnameOverride *string
	VirtualIPv4      *string
	OverrideConfig   *string
	// Clearing is separate from "not mentioned": the caller may want NULL.
	ClearHostname *bool
	ClearIPv4     *bool
}

// UpdateMember applies only the columns the patch mentions.
func (s *Store) UpdateMember(ctx context.Context, userID int64, networkID, memberID string, patch MemberPatch, now time.Time) error {
	statement := query.Table("network_members")
	if patch.ClearHostname != nil && *patch.ClearHostname {
		statement = statement.Set("hostname_override", nil)
	} else if patch.HostnameOverride != nil {
		statement = statement.Set("hostname_override", patch.HostnameOverride)
	}
	if patch.ClearIPv4 != nil && *patch.ClearIPv4 {
		statement = statement.Set("virtual_ipv4", nil)
	} else if patch.VirtualIPv4 != nil {
		statement = statement.Set("virtual_ipv4", patch.VirtualIPv4)
	}
	if patch.OverrideConfig != nil {
		statement = statement.Set("override_config", patch.OverrideConfig)
	}
	built, args := statement.
		Set("update_time", dal.TimeArg(s.Dialect(), now)).
		Where("user_id", "=", userID).Where("network_id", "=", networkID).
		Where("id", "=", memberID).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, built, args...)
	if err != nil {
		return fmt.Errorf("bal: update member: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveMember unbinds a device from a network.
func (s *Store) RemoveMember(ctx context.Context, userID int64, networkID, memberID string) error {
	statement, args := query.DeleteFrom("network_members").
		Where("user_id", "=", userID).Where("network_id", "=", networkID).
		Where("id", "=", memberID).Build(s.Dialect())
	_, err := s.conn.Exec(ctx, statement, args...)
	return err
}

// CreateCredential stores an admission credential.
func (s *Store) CreateCredential(ctx context.Context, credential NetworkCredential) error {
	statement, args := query.Into("network_credentials").
		Columns("user_id", "network_id", "credential_id", "credential_secret", "expiry_unix", "acl_groups", "allow_relay", "allowed_proxy_cidrs", "reusable", "create_time", "update_time").
		Values(credential.UserID, credential.NetworkID, credential.CredentialID, credential.CredentialSecret,
			credential.ExpiryUnix, encodeStrings(credential.ACLGroups),
			dal.BoolArg(s.Dialect(), credential.AllowRelay), encodeStrings(credential.AllowedProxyCIDRs),
			dal.BoolArg(s.Dialect(), credential.Reusable),
			dal.TimeArg(s.Dialect(), credential.CreateTime), dal.TimeArg(s.Dialect(), credential.UpdateTime)).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: create credential: %w", err)
	}
	return nil
}

// ListCredentials lists the credentials of a network.
func (s *Store) ListCredentials(ctx context.Context, userID int64, networkID string) ([]NetworkCredential, error) {
	statement, args := query.From("network_credentials").
		Columns("user_id", "network_id", "credential_id", "credential_secret", "expiry_unix", "acl_groups", "allow_relay", "allowed_proxy_cidrs", "reusable", "create_time", "update_time").
		Where("user_id", "=", userID).Where("network_id", "=", networkID).
		OrderBy("create_time", false).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list credentials: %w", err)
	}
	defer rows.Close()
	var out []NetworkCredential
	for rows.Next() {
		var (
			credential           NetworkCredential
			groups, cidrs        string
			allowRelay, reusable any
			created, updated     any
		)
		if err := rows.Scan(&credential.UserID, &credential.NetworkID, &credential.CredentialID,
			&credential.CredentialSecret, &credential.ExpiryUnix, &groups, &allowRelay, &cidrs,
			&reusable, &created, &updated); err != nil {
			return nil, fmt.Errorf("bal: scan credential: %w", err)
		}
		credential.ACLGroups = decodeStrings(groups)
		credential.AllowedProxyCIDRs = decodeStrings(cidrs)
		credential.AllowRelay = dal.ParseBool(allowRelay)
		credential.Reusable = dal.ParseBool(reusable)
		if credential.CreateTime, err = dal.ParseTime(created); err != nil {
			return nil, err
		}
		if credential.UpdateTime, err = dal.ParseTime(updated); err != nil {
			return nil, err
		}
		out = append(out, credential)
	}
	return out, rows.Err()
}

// RevokeCredential deletes an admission credential.
func (s *Store) RevokeCredential(ctx context.Context, userID int64, networkID, credentialID string) error {
	statement, args := query.DeleteFrom("network_credentials").
		Where("user_id", "=", userID).Where("network_id", "=", networkID).
		Where("credential_id", "=", credentialID).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("bal: revoke credential: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountMembersByUser counts every membership of a tenant across all networks.
// The instance quota is per user, so counting inside one network would let a
// user multiply the quota by creating more networks.
func (s *Store) CountMembersByUser(ctx context.Context, userID int64) (int, error) {
	statement := "SELECT COUNT(*) FROM " + query.Quote("network_members") +
		" WHERE " + query.Quote("user_id") + " = " + query.Placeholder(s.Dialect(), 1)
	var count int
	if err := s.conn.QueryRow(ctx, statement, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("bal: count members: %w", err)
	}
	return count, nil
}

// TenantsWithNetworks lists every tenant that owns at least one network.
func (s *Store) TenantsWithNetworks(ctx context.Context) ([]int64, error) {
	statement := "SELECT DISTINCT " + query.Quote("user_id") + " FROM " + query.Quote("networks") +
		" ORDER BY " + query.Quote("user_id")
	rows, err := s.conn.Query(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("bal: list network tenants: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AddMember binds one device; it is AddMembers with a single element.
func (s *Store) AddMember(ctx context.Context, member NetworkMember) error {
	return s.AddMembers(ctx, []NetworkMember{member})
}

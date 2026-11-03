package bal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/db/dal/query"
)

const deviceColumns = "user_id, machine_id, hostname, alias, easytier_version, device_os, client_url, first_seen_time, last_seen_time"

// UpsertDevice records a heartbeat observation. first_seen and alias stay owned
// by the console, never by the heartbeat.
func (s *Store) UpsertDevice(ctx context.Context, device Device) error {
	statement, args := query.Into("devices").
		Columns("user_id", "machine_id", "hostname", "alias", "easytier_version", "device_os", "client_url", "first_seen_time", "last_seen_time").
		Values(device.UserID, device.MachineID, device.Hostname, device.Alias, device.EasytierVersion,
			device.DeviceOS, device.ClientURL,
			dal.TimeArg(s.Dialect(), device.FirstSeen), dal.TimeArg(s.Dialect(), device.LastSeen)).
		OnConflict([]string{"user_id", "machine_id"},
			[]string{"hostname", "easytier_version", "device_os", "client_url", "last_seen_time"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: upsert device: %w", err)
	}
	return nil
}

func (s *Store) scanDevice(row dal.Row) (Device, error) {
	var (
		device Device
		first  any
		last   any
	)
	err := row.Scan(&device.UserID, &device.MachineID, &device.Hostname, &device.Alias,
		&device.EasytierVersion, &device.DeviceOS, &device.ClientURL, &first, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("bal: scan device: %w", err)
	}
	if device.FirstSeen, err = dal.ParseTime(first); err != nil {
		return Device{}, err
	}
	if device.LastSeen, err = dal.ParseTime(last); err != nil {
		return Device{}, err
	}
	return device, nil
}

// GetDevice loads one registered device.
func (s *Store) GetDevice(ctx context.Context, userID int64, machineID string) (Device, error) {
	statement, args := query.From("devices").Columns(splitColumns(deviceColumns)...).
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).Build(s.Dialect())
	return s.scanDevice(s.conn.QueryRow(ctx, statement, args...))
}

// ListDevices returns the registry, most recently seen first.
func (s *Store) ListDevices(ctx context.Context, userID int64) ([]Device, error) {
	statement, args := query.From("devices").Columns(splitColumns(deviceColumns)...).
		Where("user_id", "=", userID).OrderBy("last_seen_time", true).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list devices: %w", err)
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		device, err := s.scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, device)
	}
	return out, rows.Err()
}

// SetDeviceAlias renames a device inside the console only.
func (s *Store) SetDeviceAlias(ctx context.Context, userID int64, machineID, alias string) error {
	statement, args := query.Table("devices").Set("alias", alias).
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("bal: set alias: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteDevice removes a device and its configuration revision row.
func (s *Store) DeleteDevice(ctx context.Context, userID int64, machineID string) error {
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("bal: delete device: %w", err)
	}
	deleteMemberships, membershipArgs := query.DeleteFrom("network_members").
		Where("user_id", "=", userID).Where("device_id", "=", machineID).Build(s.Dialect())
	if _, err := tx.Exec(ctx, deleteMemberships, membershipArgs...); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("bal: delete device memberships: %w", err)
	}
	deleteDevice, deviceArgs := query.DeleteFrom("devices").
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).Build(s.Dialect())
	affected, err := tx.Exec(ctx, deleteDevice, deviceArgs...)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("bal: delete device: %w", err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return ErrNotFound
	}
	deleteRevision, revisionArgs := query.DeleteFrom("managed_config_revisions").
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).Build(s.Dialect())
	if _, err := tx.Exec(ctx, deleteRevision, revisionArgs...); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("bal: delete device revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bal: delete device: %w", err)
	}
	return nil
}

// BlockDevice records a block, keeping any earlier attempt counter.
func (s *Store) BlockDevice(ctx context.Context, userID int64, machineID, hostname string) error {
	statement, args := query.Into("blocked_devices").
		Columns("user_id", "machine_id", "hostname", "blocked_time", "attempt_count").
		Values(userID, machineID, hostname, dal.TimeArg(s.Dialect(), time.Now()), 0).
		OnConflict([]string{"user_id", "machine_id"}, []string{"hostname", "blocked_time"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: block device: %w", err)
	}
	return nil
}

// UnblockDevice lifts a block.
func (s *Store) UnblockDevice(ctx context.Context, userID int64, machineID string) error {
	statement, args := query.DeleteFrom("blocked_devices").
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordBlockedAttempt bumps the counter that feeds the console bell badge.
func (s *Store) RecordBlockedAttempt(ctx context.Context, userID int64, machineID string) error {
	statement := "UPDATE " + query.Quote("blocked_devices") +
		" SET " + query.Quote("attempt_count") + " = " + query.Quote("attempt_count") + " + 1, " +
		query.Quote("last_attempt_time") + " = " + query.Placeholder(s.Dialect(), 1) +
		" WHERE " + query.Quote("user_id") + " = " + query.Placeholder(s.Dialect(), 2) +
		" AND " + query.Quote("machine_id") + " = " + query.Placeholder(s.Dialect(), 3)
	_, err := s.conn.Exec(ctx, statement, dal.TimeArg(s.Dialect(), time.Now()), userID, machineID)
	return err
}

// IsBlocked reports whether a device is currently blocked.
func (s *Store) IsBlocked(ctx context.Context, userID int64, machineID string) (bool, error) {
	statement := "SELECT 1 FROM " + query.Quote("blocked_devices") +
		" WHERE " + query.Quote("user_id") + " = " + query.Placeholder(s.Dialect(), 1) +
		" AND " + query.Quote("machine_id") + " = " + query.Placeholder(s.Dialect(), 2)
	var one int
	err := s.conn.QueryRow(ctx, statement, userID, machineID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListBlockedDevices returns blocked devices, most recently blocked first.
func (s *Store) ListBlockedDevices(ctx context.Context, userID int64) ([]BlockedDevice, error) {
	statement, args := query.From("blocked_devices").
		Columns("user_id", "machine_id", "hostname", "blocked_time", "attempt_count", "last_attempt_time").
		Where("user_id", "=", userID).OrderBy("blocked_time", true).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list blocked: %w", err)
	}
	defer rows.Close()
	var out []BlockedDevice
	for rows.Next() {
		var (
			item        BlockedDevice
			blocked     any
			lastAttempt any
		)
		if err := rows.Scan(&item.UserID, &item.MachineID, &item.Hostname, &blocked,
			&item.AttemptCount, &lastAttempt); err != nil {
			return nil, fmt.Errorf("bal: scan blocked: %w", err)
		}
		if item.BlockedTime, err = dal.ParseTime(blocked); err != nil {
			return nil, err
		}
		if lastAttempt != nil {
			when, err := dal.ParseTime(lastAttempt)
			if err != nil {
				return nil, err
			}
			item.LastAttemptTime = &when
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// GetManagedConfigRevision reads the revision already applied to a device.
func (s *Store) GetManagedConfigRevision(ctx context.Context, userID int64, machineID string) (string, error) {
	statement, args := query.From("managed_config_revisions").Columns("config_revision").
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).Build(s.Dialect())
	var revision string
	err := s.conn.QueryRow(ctx, statement, args...).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("bal: get revision: %w", err)
	}
	return revision, nil
}

// SetManagedConfigRevision performs the compare-and-swap behind the reconcile
// loop. A nil expected revision writes unconditionally.
func (s *Store) SetManagedConfigRevision(ctx context.Context, userID int64, machineID, revision string, expected *string) error {
	if expected == nil {
		statement, args := query.Into("managed_config_revisions").
			Columns("user_id", "machine_id", "config_revision", "update_time").
			Values(userID, machineID, revision, dal.TimeArg(s.Dialect(), time.Now())).
			OnConflict([]string{"user_id", "machine_id"}, []string{"config_revision", "update_time"}).
			Build(s.Dialect())
		if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
			return fmt.Errorf("bal: set revision: %w", err)
		}
		return nil
	}
	statement, args := query.Table("managed_config_revisions").
		Set("config_revision", revision).
		Set("update_time", dal.TimeArg(s.Dialect(), time.Now())).
		Where("user_id", "=", userID).Where("machine_id", "=", machineID).
		Where("config_revision", "=", *expected).Build(s.Dialect())
	affected, err := s.conn.Exec(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("bal: cas revision: %w", err)
	}
	if affected == 0 {
		return ErrRevisionConflict
	}
	return nil
}

// UpsertRunningConfig stores the desired configuration of one instance.
func (s *Store) UpsertRunningConfig(ctx context.Context, config RunningConfig) error {
	now := time.Now()
	created := config.CreateTime
	if created.IsZero() {
		created = now
	}
	statement, args := query.Into("user_running_network_configs").
		Columns("user_id", "device_id", "network_instance_id", "network_config", "disabled", "source", "create_time", "update_time").
		Values(config.UserID, config.DeviceID, config.NetworkInstanceID, config.NetworkConfig,
			dal.BoolArg(s.Dialect(), config.Disabled), config.Source,
			dal.TimeArg(s.Dialect(), created), dal.TimeArg(s.Dialect(), now)).
		OnConflict([]string{"network_instance_id"},
			[]string{"network_config", "disabled", "source", "update_time"}).
		Build(s.Dialect())
	if _, err := s.conn.Exec(ctx, statement, args...); err != nil {
		return fmt.Errorf("bal: upsert running config: %w", err)
	}
	return nil
}

// ListRunningConfigs returns every stored instance configuration of a device.
func (s *Store) ListRunningConfigs(ctx context.Context, userID int64, deviceID string) ([]RunningConfig, error) {
	statement, args := query.From("user_running_network_configs").
		Columns("id", "user_id", "device_id", "network_instance_id", "network_config", "disabled", "source", "create_time", "update_time").
		Where("user_id", "=", userID).Where("device_id", "=", deviceID).
		OrderBy("create_time", false).Build(s.Dialect())
	rows, err := s.conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("bal: list running configs: %w", err)
	}
	defer rows.Close()
	var out []RunningConfig
	for rows.Next() {
		var (
			config           RunningConfig
			disabled         any
			created, updated any
		)
		if err := rows.Scan(&config.ID, &config.UserID, &config.DeviceID, &config.NetworkInstanceID,
			&config.NetworkConfig, &disabled, &config.Source, &created, &updated); err != nil {
			return nil, fmt.Errorf("bal: scan running config: %w", err)
		}
		config.Disabled = dal.ParseBool(disabled)
		if config.CreateTime, err = dal.ParseTime(created); err != nil {
			return nil, err
		}
		if config.UpdateTime, err = dal.ParseTime(updated); err != nil {
			return nil, err
		}
		out = append(out, config)
	}
	return out, rows.Err()
}

// DeleteRunningConfig drops one stored configuration.
func (s *Store) DeleteRunningConfig(ctx context.Context, userID int64, instanceID string) error {
	statement, args := query.DeleteFrom("user_running_network_configs").
		Where("user_id", "=", userID).Where("network_instance_id", "=", instanceID).Build(s.Dialect())
	_, err := s.conn.Exec(ctx, statement, args...)
	return err
}

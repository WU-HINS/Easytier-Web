// Package bal is the business adapter layer (HiDNS BAL).
//
// It is the only database surface business code may import: routes, session
// management and the network compiler call these operations and never see SQL,
// a dialect, a connection or a query builder.
package bal

import (
	"errors"
	"time"

	"github.com/easytier/easytier-console/internal/db/dal"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = dal.ErrNotFound

// ErrRevisionConflict is returned when a compare-and-swap on the managed
// configuration revision fails.
var ErrRevisionConflict = errors.New("bal: config revision conflict")

// Store is the business adapter facade.
type Store struct {
	conn dal.Conn
}

// New wraps a connection. Only wiring code (internal/db) constructs a Store.
func New(conn dal.Conn) *Store { return &Store{conn: conn} }

// Dialect exposes the storage dialect for reporting and for the compiler tests.
func (s *Store) Dialect() dal.Dialect { return s.conn.Dialect() }

// Close releases the pool.
func (s *Store) Close() error { return s.conn.Close() }

// User is a console account.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Email        string
	OIDCSubject  string
	OIDCIssuer   string
	DisplayName  string
	Role         string
	Disabled     bool
	CreatedAt    time.Time
}

// SessionData is one opaque console session payload.
type SessionData struct {
	SID     string
	UserID  int64
	Data    string
	Expiry  time.Time
	Created time.Time
}

// Device is one row of the durable device registry.
type Device struct {
	UserID          int64
	MachineID       string
	Hostname        string
	Alias           string
	EasytierVersion string
	DeviceOS        string
	ClientURL       string
	FirstSeen       time.Time
	LastSeen        time.Time
}

// BlockedDevice is a device refused at token validation.
type BlockedDevice struct {
	UserID          int64
	MachineID       string
	Hostname        string
	BlockedTime     time.Time
	AttemptCount    int64
	LastAttemptTime *time.Time
}

// RunningConfig is one stored instance configuration for a device.
type RunningConfig struct {
	ID                int64
	UserID            int64
	DeviceID          string
	NetworkInstanceID string
	NetworkConfig     string
	Disabled          bool
	Source            string
	CreateTime        time.Time
	UpdateTime        time.Time
}

// Network is the tenant-scoped declarative network intent.
type Network struct {
	UserID           int64
	ID               string
	DisplayName      string
	NetworkName      string
	NetworkSecret    string
	NetworkingMethod string
	PublicServerURL  *string
	PeerURLs         []string
	VirtualCIDR      *string
	SecureMode       bool
	ACLPolicy        *string
	// Advanced carries the network level settings that every member inherits;
	// a member override wins field by field.
	Advanced   *string
	CreateTime time.Time
	UpdateTime time.Time
}

// NetworkMember binds one device to a network intent.
type NetworkMember struct {
	UserID           int64
	NetworkID        string
	ID               string
	DeviceID         string
	HostnameOverride *string
	VirtualIPv4      *string
	AllocatedIPv4    *string
	OverrideConfig   *string
	CredentialID     *string
	ACLGroupSecret   *string
	CreateTime       time.Time
	UpdateTime       time.Time
}

// NetworkCredential is an admission token for a network.
type NetworkCredential struct {
	UserID            int64
	NetworkID         string
	CredentialID      string
	CredentialSecret  string
	ExpiryUnix        int64
	ACLGroups         []string
	AllowRelay        bool
	AllowedProxyCIDRs []string
	Reusable          bool
	CreateTime        time.Time
	UpdateTime        time.Time
}

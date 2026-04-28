package caddy

import (
	"bytes"
	"encoding/gob"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/dunglas/mercure"
)

func init() { //nolint:gochecknoinits
	caddy.RegisterModule(Redis{})
}

// Redis represents a Redis transport configuration for Mercure.
// Supports both standalone and cluster modes.
type Redis struct {
	// Addresses of Redis nodes (can be single node or cluster nodes)
	Addresses []string `json:"addresses,omitempty"`

	// Password for Redis authentication (optional)
	Password string `json:"password,omitempty"`

	// Database number (only used in standalone mode)
	DB int `json:"db,omitempty"`

	// Key prefix for all Redis keys
	KeyPrefix string `json:"key_prefix,omitempty"`

	// Channel prefix for Pub/Sub channels
	ChannelPrefix string `json:"channel_prefix,omitempty"`

	// Maximum number of updates to keep in history
	Size uint64 `json:"size,omitempty"`

	// Probability of cleanup on each update (0-1)
	CleanupFrequency float64 `json:"cleanup_frequency,omitempty"`

	transport    *mercure.RedisTransport
	transportKey string
}

// CaddyModule returns the Caddy module information.
func (Redis) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.mercure.redis",
		New: func() caddy.Module { return new(Redis) },
	}
}

func (r *Redis) GetTransport() mercure.Transport { //nolint:ireturn
	return r.transport
}

// Provision provisions r's configuration.
//
//nolint:wrapcheck
func (r *Redis) Provision(ctx caddy.Context) error {
	// Set defaults
	if len(r.Addresses) == 0 {
		r.Addresses = []string{"localhost:6379"}
	}

	if r.KeyPrefix == "" {
		r.KeyPrefix = "mercure:"
	}

	if r.ChannelPrefix == "" {
		r.ChannelPrefix = "mercure:updates"
	}

	if r.CleanupFrequency == 0 {
		r.CleanupFrequency = mercure.RedisDefaultCleanupFrequency
	}

	var key bytes.Buffer
	if err := gob.NewEncoder(&key).Encode(r); err != nil {
		return err
	}

	r.transportKey = key.String()

	destructor, _, err := TransportUsagePool.LoadOrNew(r.transportKey, func() (caddy.Destructor, error) {
		config := mercure.RedisTransportConfig{
			Addrs:            r.Addresses,
			Password:         r.Password,
			DB:               r.DB,
			KeyPrefix:        r.KeyPrefix,
			ChannelPrefix:    r.ChannelPrefix,
			Size:             r.Size,
			CleanupFrequency: r.CleanupFrequency,
		}

		t, err := mercure.NewRedisTransport(
			mercure.NewSubscriberList(ctx.Value(SubscriberListCacheSizeContextKey).(int)),
			ctx.Slogger(),
			config,
		)
		if err != nil {
			return nil, err
		}

		return TransportDestructor[*mercure.RedisTransport]{Transport: t}, nil
	})
	if err != nil {
		return err
	}

	r.transport = destructor.(TransportDestructor[*mercure.RedisTransport]).Transport

	return nil
}

//nolint:wrapcheck
func (r *Redis) Cleanup() error {
	_, err := TransportUsagePool.Delete(r.transportKey)

	return err
}

// UnmarshalCaddyfile sets up the handler from Caddyfile tokens.
//
//nolint:wrapcheck
func (r *Redis) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "addresses":
				r.Addresses = d.RemainingArgs()
				if len(r.Addresses) == 0 {
					return d.ArgErr()
				}

			case "password":
				if !d.NextArg() {
					return d.ArgErr()
				}

				r.Password = d.Val()

			case "db":
				if !d.NextArg() {
					return d.ArgErr()
				}

				db, err := strconv.Atoi(d.Val())
				if err != nil {
					return d.WrapErr(err)
				}

				r.DB = db

			case "key_prefix":
				if !d.NextArg() {
					return d.ArgErr()
				}

				r.KeyPrefix = d.Val()

			case "channel_prefix":
				if !d.NextArg() {
					return d.ArgErr()
				}

				r.ChannelPrefix = d.Val()

			case "size":
				if !d.NextArg() {
					return d.ArgErr()
				}

				size, err := strconv.ParseUint(d.Val(), 10, 64)
				if err != nil {
					return d.WrapErr(err)
				}

				r.Size = size

			case "cleanup_frequency":
				if !d.NextArg() {
					return d.ArgErr()
				}

				freq, err := strconv.ParseFloat(d.Val(), 64)
				if err != nil {
					return d.WrapErr(err)
				}

				r.CleanupFrequency = freq
			}
		}
	}

	return nil
}

var (
	_ caddy.Provisioner     = (*Redis)(nil)
	_ caddy.CleanerUpper    = (*Redis)(nil)
	_ caddyfile.Unmarshaler = (*Redis)(nil)
)

package mercure

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const RedisDefaultCleanupFrequency = 0.3

// RedisTransport implements the TransportInterface using Redis.
// It supports both standalone and cluster modes.
type RedisTransport struct {
	sync.RWMutex

	subscribers      *SubscriberList
	logger           *slog.Logger
	client           redis.UniversalClient
	keyPrefix        string
	channelPrefix    string
	size             uint64
	cleanupFrequency float64
	closed           chan struct{}
	closedOnce       sync.Once
	lastSeq          uint64
	lastEventID      string
	pubSub           redis.PubSub
	cancelPubSub     context.CancelFunc
}

// RedisTransportConfig holds configuration for RedisTransport.
type RedisTransportConfig struct {
	// Standalone addresses or cluster addresses
	Addrs []string
	// Password for authentication (optional)
	Password string
	// Database number (only for standalone mode)
	DB int
	// Key prefix for all Redis keys
	KeyPrefix string
	// Channel prefix for Pub/Sub channels
	ChannelPrefix string
	// Maximum number of updates to keep in history
	Size uint64
	// Probability of cleanup on each update (0-1)
	CleanupFrequency float64
}

// NewRedisTransport creates a new RedisTransport with standalone configuration.
func NewRedisTransport(
	subscriberList *SubscriberList,
	logger *slog.Logger,
	config RedisTransportConfig,
) (*RedisTransport, error) {
	if len(config.Addrs) == 0 {
		config.Addrs = []string{"localhost:6379"}
	}

	if config.KeyPrefix == "" {
		config.KeyPrefix = "mercure:"
	}

	if config.ChannelPrefix == "" {
		config.ChannelPrefix = "mercure:updates"
	}

	if config.CleanupFrequency == 0 {
		config.CleanupFrequency = RedisDefaultCleanupFrequency
	}

	// Create Redis client (handles both standalone and cluster mode)
	client := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:    config.Addrs,
		Password: config.Password,
		DB:       config.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil; &TransportError{err: fmt.Errorf("unable to connect to Redis: %w", err)}
	}

	lastEventID, err := getRedisLastEventID(ctx, client, config.KeyPrefix)
	if err != nil {
		client.Close()
		return nil, &TransportError{err: err}
	}

	rt := &RedisTransport{
		logger:           logger,
		client:           client,
		keyPrefix:        config.KeyPrefix,
		channelPrefix:    config.ChannelPrefix,
		size:             config.Size,
		cleanupFrequency: config.CleanupFrequency,
		subscribers:      subscriberList,
		closed:           make(chan struct{}),
		lastEventID:      lastEventID,
	}

	// Start Pub/Sub listener in the background
	go rt.startPubSubListener()

	return rt, nil
}

// NewRedisClusterTransport creates a new RedisTransport configured for cluster mode.
func NewRedisClusterTransport(
	subscriberList *SubscriberList,
	logger *slog.Logger,
	clusterAddrs []string,
	password string,
	keyPrefix string,
	channelPrefix string,
	size uint64,
	cleanupFrequency float64,
) (*RedisTransport, error) {
	config := RedisTransportConfig{
		Addrs:            clusterAddrs,
		Password:         password,
		KeyPrefix:        keyPrefix,
		ChannelPrefix:    channelPrefix,
		Size:             size,
		CleanupFrequency: cleanupFrequency,
	}

	if config.KeyPrefix == "" {
		config.KeyPrefix = "mercure:"
	}

	if config.ChannelPrefix == "" {
		config.ChannelPrefix = "mercure:updates"
	}

	return NewRedisTransport(subscriberList, logger, config)
}

func getRedisLastEventID(ctx context.Context, client redis.UniversalClient, keyPrefix string) (string, error) {
	key := keyPrefix + "events"

	// Get the last key from the sorted set
	results, err := client.ZRevRange(ctx, key, 0, 0).Result()
	if err != nil {
		return "", fmt.Errorf("unable to get lastEventID from Redis: %w", err)
	}

	if len(results) == 0 {
		return EarliestLastEventID, nil
	}

	// Extract event ID from the key (format: "seq:eventid")
	parts := bytes.Split([]byte(results[0]), []byte(":"))
	if len(parts) < 2 {
		return EarliestLastEventID, nil
	}

	return string(parts[1]), nil
}

// Dispatch dispatches an update to all subscribers and persists it in Redis.
func (t *RedisTransport) Dispatch(ctx context.Context, update *Update) error {
	select {
	case <-t.closed:
		return ErrClosedTransport
	default:
	}

	update.AssignUUID()

	updateJSON, err := json.Marshal(*update)
	if err != nil {
		return fmt.Errorf("error when marshaling update: %w", err)
	}

	t.Lock()
	defer t.Unlock()

	if err := t.persist(ctx, update.ID, updateJSON); err != nil {
		return err
	}

	// Publish to subscribers listening on this transport
	for _, s := range t.subscribers.MatchAny(update) {
		s.Dispatch(ctx, update, false)
	}

	// Publish to Redis Pub/Sub for other instances
	if err := t.publishUpdate(ctx, update, updateJSON); err != nil {
		if t.logger.Enabled(ctx, slog.LevelWarn) {
			t.logger.LogAttrs(ctx, slog.LevelWarn, "Failed to publish update to Redis Pub/Sub",
				slog.Any("error", err))
		}
	}

	return nil
}

// publishUpdate publishes an update to Redis Pub/Sub channel for other instances.
func (t *RedisTransport) publishUpdate(ctx context.Context, update *Update, updateJSON []byte) error {
	// Serialize update with topics and private flag for remote subscribers
	type remoteUpdate struct {
		Update   []byte
		Topics   []string
		Private  bool
		Sequence uint64
	}

	payload := remoteUpdate{
		Update:   updateJSON,
		Topics:   update.Topics,
		Private:  update.Private,
		Sequence: t.lastSeq,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshaling remote update: %w", err)
	}

	return t.client.Publish(ctx, t.channelPrefix, payloadJSON).Err()
}

// AddSubscriber adds a new subscriber to the transport.
func (t *RedisTransport) AddSubscriber(ctx context.Context, s *LocalSubscriber) error {
	select {
	case <-t.closed:
		return ErrClosedTransport
	default:
	}

	t.Lock()
	t.subscribers.Add(s)
	toSeq := t.lastSeq
	t.Unlock()

	if s.RequestLastEventID != "" {
		if err := t.dispatchHistory(ctx, s, toSeq); err != nil {
			return err
		}
	}

	s.Ready(ctx)

	return nil
}

// RemoveSubscriber removes a subscriber from the transport.
func (t *RedisTransport) RemoveSubscriber(_ context.Context, s *LocalSubscriber) error {
	select {
	case <-t.closed:
		return ErrClosedTransport
	default:
	}

	t.Lock()
	defer t.Unlock()

	t.subscribers.Remove(s)

	return nil
}

// GetSubscribers gets the list of active subscribers.
func (t *RedisTransport) GetSubscribers(_ context.Context) (string, []*Subscriber, error) {
	t.RLock()
	defer t.RUnlock()

	return t.lastEventID, getSubscribers(t.subscribers), nil
}

// Close closes the Transport.
func (t *RedisTransport) Close(ctx context.Context) error {
	var err error
	t.closedOnce.Do(func() {
		close(t.closed)

		t.Lock()
		defer t.Unlock()

		// Cancel Pub/Sub listener
		if t.cancelPubSub != nil {
			t.cancelPubSub()
		}

		// Disconnect all subscribers
		t.subscribers.Walk(0, func(s *LocalSubscriber) bool {
			s.Disconnect()
			return true
		})

		// Close Redis connection
		if closeErr := t.client.Close(); closeErr != nil {
			err = fmt.Errorf("unable to close Redis connection: %w", closeErr)
		}
	})

	return err
}

// Ready reports whether the transport can currently serve traffic.
// Implements TransportHealthChecker.
func (t *RedisTransport) Ready(ctx context.Context) error {
	select {
	case <-t.closed:
		return ErrClosedTransport
	default:
	}

	// Check if we can reach Redis
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	return t.client.Ping(ctx).Err()
}

// Live reports whether the transport is fundamentally operational.
// Implements TransportHealthChecker.
func (t *RedisTransport) Live(ctx context.Context) error {
	select {
	case <-t.closed:
		return fmt.Errorf("transport is closed")
	default:
	}

	// Check if we can reach Redis with a longer timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return t.client.Ping(ctx).Err()
}

// startPubSubListener starts the Pub/Sub listener to receive updates from other instances.
func (t *RedisTransport) startPubSubListener() {
	ctx, cancel := context.WithCancel(context.Background())
	t.cancelPubSub = cancel

	pubSub := t.client.Subscribe(ctx, t.channelPrefix)
	defer pubSub.Close()

	ch := pubSub.Channel()

	for {
		select {
		case <-t.closed:
			return
		case msg := <-ch:
			if msg == nil {
				return
			}

			t.handleRemoteUpdate(context.Background(), msg.Payload)
		}
	}
}

// handleRemoteUpdate processes updates received from other instances via Pub/Sub.
func (t *RedisTransport) handleRemoteUpdate(ctx context.Context, payload string) {
	type remoteUpdate struct {
		Update   []byte
		Topics   []string
		Private  bool
		Sequence uint64
	}

	var remote remoteUpdate
	if err := json.Unmarshal([]byte(payload), &remote); err != nil {
		if t.logger.Enabled(ctx, slog.LevelError) {
			t.logger.LogAttrs(ctx, slog.LevelError, "Failed to unmarshal remote update",
				slog.Any("error", err))
		}
		return
	}

	var update *Update
	if err := json.Unmarshal(remote.Update, &update); err != nil {
		if t.logger.Enabled(ctx, slog.LevelError) {
			t.logger.LogAttrs(ctx, slog.LevelError, "Failed to unmarshal update from remote",
				slog.Any("error", err))
		}
		return
	}

	// Dispatch to local subscribers that match
	t.RLock()
	for _, s := range t.subscribers.MatchAny(update) {
		s.Dispatch(ctx, update, false)
	}
	t.RUnlock()
}

// dispatchHistory dispatches historical updates to a subscriber.
func (t *RedisTransport) dispatchHistory(ctx context.Context, s *LocalSubscriber, toSeq uint64) error {
	key := t.keyPrefix + "events"

	// Find the start index based on RequestLastEventID
	var startIdx int64 = -1
	if s.RequestLastEventID != EarliestLastEventID {
		// Get all scores to find the starting point
		results, err := t.client.ZRangeByScore(ctx, key, &redis.ZRangeByScore{
			Min: "-inf",
			Max: "+inf",
		}).Result()
		if err != nil {
			return fmt.Errorf("unable to retrieve history from Redis: %w", err)
		}

		for i, eventKey := range results {
			parts := bytes.Split([]byte(eventKey), []byte(":"))
			if len(parts) >= 2 && string(parts[1]) == s.RequestLastEventID {
				startIdx = int64(i)
				break
			}
		}

		if startIdx == -1 {
			// Event ID not found, log and use earliest
			if t.logger.Enabled(ctx, slog.LevelInfo) {
				t.logger.LogAttrs(ctx, slog.LevelInfo, "Can't find requested LastEventID")
			}
			startIdx = 0
		} else {
			startIdx++ // Start after the requested event
		}
	} else {
		startIdx = 0
	}

	// Retrieve events from Redis sorted set
	results, err := t.client.ZRange(ctx, key, startIdx, -1).Result()
	if err != nil {
		return fmt.Errorf("unable to retrieve history from Redis: %w", err)
	}

	responseLastEventID := EarliestLastEventID

	for _, eventKey := range results {
		// Respect the sequence bound
		seq, err := parseSequenceFromKey(eventKey)
		if err != nil {
			continue
		}

		if seq > toSeq {
			break
		}

		parts := bytes.Split([]byte(eventKey), []byte(":"))
		if len(parts) >= 2 {
			responseLastEventID = string(parts[1])
		}

		// Get the update JSON from the hash
		updateJSON, err := t.client.HGet(ctx, eventKey, "data").Result()
		if err != nil && err != redis.Nil {
			if t.logger.Enabled(ctx, slog.LevelError) {
				t.logger.LogAttrs(ctx, slog.LevelError, "Failed to retrieve update data from Redis",
					slog.Any("error", err))
			}
			continue
		}

		var update *Update
		if err := json.Unmarshal([]byte(updateJSON), &update); err != nil {
			s.HistoryDispatched(responseLastEventID)

			if t.logger.Enabled(ctx, slog.LevelError) {
				t.logger.LogAttrs(ctx, slog.LevelError, "Unable to unmarshal update from Redis",
					slog.Any("error", err))
			}

			return err
		}

		if s.Match(update) && !s.Dispatch(ctx, update, true) {
			s.HistoryDispatched(responseLastEventID)
			return nil
		}
	}

	s.HistoryDispatched(responseLastEventID)

	return nil
}

// persist stores an update in Redis.
func (t *RedisTransport) persist(ctx context.Context, updateID string, updateJSON []byte) error {
	key := t.keyPrefix + "events"

	// Get next sequence number using Redis incr
	seqResult, err := t.client.Incr(ctx, t.keyPrefix+"seq").Result()
	if err != nil {
		return fmt.Errorf("error generating sequence: %w", err)
	}

	seq := uint64(seqResult)
	eventKey := fmt.Sprintf("%d:%s", seq, updateID)

	// Store in sorted set with sequence as score
	pipe := t.client.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(seq), Member: eventKey})
	pipe.HSet(ctx, eventKey, "data", string(updateJSON))
	pipe.Expire(ctx, eventKey, 24*time.Hour) // TTL to prevent unbounded growth

	t.lastSeq = seq
	t.lastEventID = updateID

	if err := t.cleanup(ctx, pipe, key, seq); err != nil {
		return err
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis error: %w", err)
	}

	return nil
}

// cleanup removes entries in the history above the size limit, triggered probabilistically.
func (t *RedisTransport) cleanup(ctx context.Context, pipe redis.Pipeliner, key string, lastID uint64) error {
	if t.size == 0 ||
		t.cleanupFrequency == 0 ||
		t.size >= lastID ||
		(t.cleanupFrequency != 1 && rand.Float64() < t.cleanupFrequency) { //nolint:gosec
		return nil
	}

	removeUntil := float64(lastID - t.size)

	// Remove entries from sorted set that are older than the size limit
	if err := pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("(%.0f", removeUntil)).Err(); err != nil {
		return fmt.Errorf("unable to cleanup Redis: %w", err)
	}

	return nil
}

// parseSequenceFromKey extracts the sequence number from an event key.
func parseSequenceFromKey(eventKey string) (uint64, error) {
	parts := bytes.Split([]byte(eventKey), []byte(":"))
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid event key format")
	}

	var seq uint64
	_, err := fmt.Sscanf(string(parts[0]), "%d", &seq)
	if err != nil {
		return 0, fmt.Errorf("unable to parse sequence: %w", err)
	}

	return seq, nil
}

// Interface guards.
var (
	_ Transport            = (*RedisTransport)(nil)
	_ TransportSubscribers = (*RedisTransport)(nil)
	_ TransportHealthChecker = (*RedisTransport)(nil)
)
